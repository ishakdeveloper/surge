package sim

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strconv"
	"time"

	"github.com/ishakdeveloper/surge/shared/geo"
	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/ishakdeveloper/surge/shared/tracing"
	"github.com/ishakdeveloper/surge/shared/wire"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel/attribute"
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
	// Run tells this simulator run's trips apart from every other run's.
	//
	// The seed makes demand reproducible — the same pickups in the same order —
	// so on its own it also repeats trip ids, and those double as idempotency
	// keys. A matcher that outlived a simulator restart would drop the new
	// run's requests as duplicates of the old one's for its whole dedupe
	// window, and a benchmark reading trip.events counted a thousand trips
	// "matched twice" that were two runs sharing names.
	Run uint64
	// HotspotShare is the fraction of pickups drawn near one of Hotspots
	// rather than anywhere. Uniform demand spreads riders so thin that no two
	// ever want the same car — the one case in which how you match matters.
	HotspotShare float64
}

// Hotspots are where concentrated demand comes from: a station, a business
// district, a nightlife square.
var Hotspots = []geo.Point{
	{Lat: 52.3791, Lng: 4.9003}, // Centraal
	{Lat: 52.3389, Lng: 4.8723}, // Zuid
	{Lat: 52.3643, Lng: 4.8828}, // Leidseplein
}

// hotspotRadius is how far from a hotspot its pickups fall, in metres.
const hotspotRadius = 800.0

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

	// policy is the driver behaviour, shared with the WebSocket transport so
	// moving the fleet onto sockets changes the transport and nothing else.
	policy *OfferPolicy
}

func NewRiders(cluster kafkax.Cluster, group string, pool *RoutePool, config RiderConfig, policy *OfferPolicy, hooks RiderHooks) (*Riders, error) {
	producer, err := kafkax.NewProducer(cluster)
	if err != nil {
		return nil, err
	}

	// Offers are ephemeral: an offer produced while nobody was listening is one
	// whose deadline has almost certainly passed, so starting at the end is
	// right rather than merely convenient.
	consumer, err := kafkax.NewConsumerGroup(cluster, group, []string{kafkax.TopicWSPush},
		kgo.ConsumeResetOffset(kgo.NewOffset().AtEnd()))
	if err != nil {
		producer.Close()
		return nil, err
	}

	return &Riders{
		producer: producer, consumer: consumer, pool: pool, config: config, hooks: hooks,
		policy: policy,
	}, nil
}

// pickup draws where a rider asks from. The hotspot draw happens only when a
// share is set, so a run without hotspots consumes the seeded source exactly
// as it always did and stays comparable with older runs.
func (r *Riders) pickup(source *rand.Rand) (geo.Point, bool) {
	if r.config.HotspotShare > 0 && source.Float64() < r.config.HotspotShare {
		centre := Hotspots[source.IntN(len(Hotspots))]
		if point, ok := r.pool.PointNear(source, centre, hotspotRadius); ok {
			return point, true
		}
	}
	return r.pool.RandomPoint(source)
}

// TripID names a simulated trip: the seed, the run, and its place in the run.
func TripID(seed, run, sequence uint64) string {
	return fmt.Sprintf("trip-%d-%s-%d", seed, strconv.FormatUint(run, 36), sequence)
}

func (r *Riders) Close() {
	r.producer.Close()
	r.consumer.Close()
}

// Run generates demand, and answers offers only when asked to.
//
// Over WebSockets the drivers answer on their own sockets, which is the whole
// point of that transport. Having this consume ws.push as well would answer
// every offer twice — once as a socket and once as a Kafka consumer — and the
// second answer would arrive for a reservation that no longer exists.
func (r *Riders) Run(ctx context.Context, answerOffers bool) error {
	if answerOffers {
		go r.answer(ctx)
	}
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

		pickup, ok := r.pickup(source)
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
		tripID := TripID(r.config.Seed, r.config.Run, sequence)
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

		// The root span of a trip. Everything downstream — ingest, the shard
		// that matches it, the offer, the driver's answer — hangs off this one,
		// which is what makes "what happened to trip-1234" a single query.
		requestCtx, span := tracing.Tracer("sim").Start(ctx, "rider.request")
		span.SetAttributes(
			attribute.String("surge.trip", tripID),
			attribute.String("surge.cell", cell.String()),
		)

		r.emit(requestCtx, kafkax.TopicGeoEvents, cell.String(), event)
		span.End()

		if r.hooks.OnRequest != nil {
			r.hooks.OnRequest()
		}
	}
}

// answer plays the drivers.
func (r *Riders) answer(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}

		fetches := r.consumer.PollRecords(ctx, 2_000)
		if fetches.IsClientClosed() {
			return
		}

		fetches.EachRecord(func(record *kgo.Record) {
			// Continues the trip's trace into the driver's decision, so the
			// seconds a human spends deciding are visible as a span rather than
			// as an unexplained gap.
			offerCtx, span := tracing.Consume(ctx, record, "driver.offer")
			defer span.End()

			// ws.push carries whole ServerMessages now — offers and trip updates
			// on one topic — so the driver side reads the envelope and keeps
			// only what a driver answers.
			var message wire.ServerMessage
			if err := json.Unmarshal(record.Value, &message); err != nil {
				return
			}
			if message.Tag != wire.TagOffer || message.Offer == nil {
				return
			}
			offer := *message.Offer

			if r.hooks.OnOffer != nil {
				r.hooks.OnOffer()
			}

			decision := r.policy.Decide(offer)
			if decision.Duplicate {
				if r.hooks.OnDuplicate != nil {
					r.hooks.OnDuplicate(offer.TripID)
				}
				go r.reply(offerCtx, offer, false, 0)
				return
			}

			span.SetAttributes(
				attribute.String("surge.trip", offer.TripID),
				attribute.String("surge.driver", offer.DriverID),
				attribute.Bool("surge.accepted", decision.Accept),
			)

			go r.reply(offerCtx, offer, decision.Accept, decision.Delay)
		})
	}
}

func (r *Riders) reply(ctx context.Context, offer wire.Offer, accepted bool, delay time.Duration) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(delay):
	}

	if accepted && !r.policy.Commit(offer) {
		// Somebody took it while this driver was thinking. That is the race
		// worth detecting, and it can only be seen here rather than at decision
		// time.
		if r.hooks.OnDuplicate != nil {
			r.hooks.OnDuplicate(offer.TripID)
		}
		accepted = false
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

	tracing.Produce(ctx, r.producer, &kgo.Record{
		Topic: topic,
		Key:   []byte(key),
		Value: payload,
	}, func(_ *kgo.Record, err error) {
		if err != nil && r.hooks.OnError != nil {
			r.hooks.OnError(err)
		}
	})
}
