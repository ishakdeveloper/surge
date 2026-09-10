package sim

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/ishakdeveloper/surge/pkg/geo"
	"github.com/ishakdeveloper/surge/pkg/kafkax"
	"github.com/ishakdeveloper/surge/pkg/wire"
	"github.com/twmb/franz-go/pkg/kgo"
)

// RiderConfig is the demand side of the knob.
type RiderConfig struct {
	// RequestsPerSecond is the arrival rate. Combined with fleet size this is
	// what sets the supply/demand ratio, and therefore how much contention the
	// matcher actually sees — the number that makes the sharding worth having.
	RequestsPerSecond float64
	// AcceptRate is the probability a driver takes an offer. Below 1.0 the
	// matcher has to walk its candidate list, which is where the retry path and
	// the reservation-release path get exercised.
	AcceptRate float64
	// AcceptLatency is how long a driver takes to answer. Real drivers are
	// slow, and the offer is holding a reservation the whole time.
	AcceptLatency time.Duration
	Seed          uint64
}

func DefaultRiderConfig() RiderConfig {
	return RiderConfig{
		RequestsPerSecond: 5,
		AcceptRate:        0.7,
		AcceptLatency:     1500 * time.Millisecond,
		Seed:              1,
	}
}

// RiderHooks are the observability seams.
type RiderHooks struct {
	OnRequest  func()
	OnOffer    func()
	OnAccepted func()
	OnDeclined func()
	OnError    func(error)
	// OnDuplicate fires when a trip that has already been accepted is offered
	// again — the failure this whole phase exists to make impossible.
	OnDuplicate func(tripID string)
}

// Riders generates demand and answers offers on the drivers' behalf.
//
// Two halves of one loop: it asks for rides, and it plays the drivers who are
// offered them. Phase 3 moves the answering half onto a real WebSocket, at
// which point this becomes 10,000 connections instead of one consumer — but the
// behaviour it simulates does not change, which is the point of keeping them
// together here.
type Riders struct {
	producer *kgo.Client
	consumer *kgo.Client
	pool     *RoutePool
	config   RiderConfig
	hooks    RiderHooks

	// accepted is the double-dispatch detector.
	//
	// A trip may legitimately be offered to several drivers in turn — that is
	// what happens when one declines. What must never happen is two drivers
	// holding live offers for the same trip, because both could accept and the
	// rider would be assigned twice. Watching from the driver side is the
	// honest place to check it: this is what a real fleet would experience.
	mu       sync.Mutex
	accepted map[string]string
}

func NewRiders(brokers []string, group string, pool *RoutePool, config RiderConfig, hooks RiderHooks) (*Riders, error) {
	producer, err := kafkax.NewProducer(brokers)
	if err != nil {
		return nil, err
	}

	// Offers are ephemeral: an offer produced while nobody was listening is one
	// whose deadline has almost certainly passed, so starting at the end is
	// right rather than merely convenient.
	consumer, err := kafkax.NewConsumerGroup(brokers, group, []string{kafkax.TopicWSPush},
		kgo.ConsumeResetOffset(kgo.NewOffset().AtEnd()))
	if err != nil {
		producer.Close()
		return nil, err
	}

	return &Riders{
		producer: producer, consumer: consumer, pool: pool, config: config, hooks: hooks,
		accepted: make(map[string]string),
	}, nil
}

func (r *Riders) Close() {
	r.producer.Close()
	r.consumer.Close()
}

// Run generates requests and answers offers until ctx is done.
func (r *Riders) Run(ctx context.Context) error {
	go r.answer(ctx)
	return r.request(ctx)
}

func (r *Riders) request(ctx context.Context) error {
	if r.config.RequestsPerSecond <= 0 {
		<-ctx.Done()
		return nil
	}

	source := rand.New(rand.NewPCG(r.config.Seed, 0x21de5))
	interval := time.Duration(float64(time.Second) / r.config.RequestsPerSecond)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var sequence uint64

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}

		pickup, ok := r.pool.RandomPoint(source)
		if !ok {
			continue
		}
		drop, ok := r.pool.RandomPoint(source)
		if !ok {
			continue
		}

		cell, err := geo.ShardCell(pickup)
		if err != nil {
			continue
		}

		sequence++
		tripID := fmt.Sprintf("trip-%d-%d", r.config.Seed, sequence)
		now := time.Now()

		event := wire.GeoEvent{
			Tag:  wire.TagMatchRequested,
			Cell: cell.String(),
			AtMs: now.UnixMilli(),
			Requested: &wire.MatchRequestPayload{
				TripID:        tripID,
				RiderID:       fmt.Sprintf("rider-%d", sequence),
				PickupLat:     pickup.Lat,
				PickupLng:     pickup.Lng,
				DropLat:       drop.Lat,
				DropLng:       drop.Lng,
				RequestedAtMs: now.UnixMilli(),
				// The trip id doubles as the idempotency key here. A real rider
				// app would generate one client-side so a retried submit is
				// recognised; the property being exercised is the same.
				IdempotencyKey: tripID,
			},
		}

		r.emit(ctx, kafkax.TopicGeoEvents, cell.String(), event)

		if r.hooks.OnRequest != nil {
			r.hooks.OnRequest()
		}
	}
}

// answer plays the drivers.
func (r *Riders) answer(ctx context.Context) {
	source := rand.New(rand.NewPCG(r.config.Seed, 0xd21e5))

	for {
		if ctx.Err() != nil {
			return
		}

		fetches := r.consumer.PollRecords(ctx, 2_000)
		if fetches.IsClientClosed() {
			return
		}

		fetches.EachRecord(func(record *kgo.Record) {
			var offer wire.Offer
			if err := json.Unmarshal(record.Value, &offer); err != nil {
				return
			}
			if offer.Tag != wire.TagOffer {
				return
			}

			if r.hooks.OnOffer != nil {
				r.hooks.OnOffer()
			}

			// Already taken by another driver? A real driver app would refuse,
			// and so does this — but the fact that it was offered at all is the
			// thing worth counting.
			r.mu.Lock()
			holder, taken := r.accepted[offer.TripID]
			r.mu.Unlock()

			if taken && holder != offer.DriverID {
				if r.hooks.OnDuplicate != nil {
					r.hooks.OnDuplicate(offer.TripID)
				}
				go r.reply(ctx, offer, false, 0)
				return
			}

			accepted := source.Float64() < r.config.AcceptRate
			// Jittered around the configured latency, because a fleet that all
			// answers at exactly 1500ms would make the offer deadline a cliff
			// rather than a distribution.
			delay := time.Duration(float64(r.config.AcceptLatency) * (0.5 + source.Float64()))

			// One goroutine per offer, so a slow answer does not hold up the
			// rest of the batch. Offers are rare relative to pings.
			go r.reply(ctx, offer, accepted, delay)
		})
	}
}

func (r *Riders) reply(ctx context.Context, offer wire.Offer, accepted bool, delay time.Duration) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(delay):
	}

	if accepted {
		r.mu.Lock()
		if holder, taken := r.accepted[offer.TripID]; taken && holder != offer.DriverID {
			r.mu.Unlock()
			if r.hooks.OnDuplicate != nil {
				r.hooks.OnDuplicate(offer.TripID)
			}
			accepted = false
		} else {
			r.accepted[offer.TripID] = offer.DriverID
			r.mu.Unlock()
		}
	}

	event := wire.GeoEvent{
		Tag: wire.TagOfferReplied,
		// Addressed to the cell that made the reservation, not to wherever the
		// driver is now. By the time a driver answers they may have moved to a
		// cell owned by a different instance, which holds no reservation for
		// this trip and could do nothing with the reply.
		Cell: offer.ReplyCell,
		AtMs: time.Now().UnixMilli(),
		Replied: &wire.OfferRepliedPayload{
			TripID:   offer.TripID,
			DriverID: offer.DriverID,
			Accepted: accepted,
		},
	}

	r.emit(ctx, kafkax.TopicGeoEvents, offer.ReplyCell, event)

	switch {
	case accepted && r.hooks.OnAccepted != nil:
		r.hooks.OnAccepted()
	case !accepted && r.hooks.OnDeclined != nil:
		r.hooks.OnDeclined()
	}
}

func (r *Riders) emit(ctx context.Context, topic, key string, message any) {
	payload, err := json.Marshal(message)
	if err != nil {
		if r.hooks.OnError != nil {
			r.hooks.OnError(err)
		}
		return
	}

	r.producer.Produce(ctx, &kgo.Record{
		Topic: topic,
		Key:   []byte(key),
		Value: payload,
	}, func(_ *kgo.Record, err error) {
		if err != nil && r.hooks.OnError != nil {
			r.hooks.OnError(err)
		}
	})
}
