package events

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/ishakdeveloper/surge/services/chat/internal/service"
	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/ishakdeveloper/surge/shared/tracing"
	"github.com/ishakdeveloper/surge/shared/wire"
	"github.com/twmb/franz-go/pkg/kgo"
)

// Hooks are the observability seams.
type Hooks struct {
	// OnApplied counts a fact acted on, by tag.
	OnApplied func(tag string)
	// OnSkipped counts a fact moved past, by reason: one chat has no use for,
	// or one that could not be decoded.
	OnSkipped func(reason string)
	// OnRetry counts an attempt that failed and will be retried.
	OnRetry func()
}

// TripApplier is what a trip fact is applied to.
type TripApplier interface {
	TripChanged(ctx context.Context, trip service.Trip) error
}

// Lifecycle opens and closes trip conversations from `trip.lifecycle`.
type Lifecycle struct {
	client *kgo.Client
	chat   TripApplier
	hooks  Hooks
}

func NewLifecycle(brokers []string, group string, chat TripApplier, hooks Hooks) (*Lifecycle, error) {
	client, err := kafkax.NewConsumerGroup(brokers, group, []string{kafkax.TopicTripLifecycle})
	if err != nil {
		return nil, err
	}
	return &Lifecycle{client: client, chat: chat, hooks: hooks}, nil
}

func (l *Lifecycle) Close() { l.client.Close() }

func (l *Lifecycle) Run(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return nil
		}

		fetches := l.client.PollRecords(ctx, 100)
		if fetches.IsClientClosed() {
			return nil
		}
		fetches.EachRecord(func(record *kgo.Record) {
			if ctx.Err() == nil {
				l.handle(ctx, record)
			}
		})

		// A batch handled to the end is committed; one cut short by shutdown is
		// redelivered, which applying a fact twice survives.
		if ctx.Err() != nil {
			return nil
		}
		if err := l.client.CommitUncommittedOffsets(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("chat lifecycle commit failed", "error", err)
		}
	}
}

// TripOf is the trip a fact describes, and whether it is one chat acts on:
// an acceptance opens a conversation, and an end with a driver closes one.
// A request, or a trip that ended before anybody accepted it, has nobody to
// talk to.
func TripOf(fact wire.TripFact) (service.Trip, bool) {
	trip := service.Trip{ID: fact.TripID, RiderID: fact.RiderID, DriverID: fact.DriverID}
	switch fact.Tag {
	case wire.FactTripAccepted:
		return trip, true
	case wire.FactTripCompleted, wire.FactTripCancelled:
		if fact.DriverID == "" {
			return service.Trip{}, false
		}
		trip.EndedAt = time.UnixMilli(fact.AtMs)
		return trip, true
	default:
		return service.Trip{}, false
	}
}

// handle applies one fact, retrying in place until it lands or the service
// stops. Retried rather than skipped, as payments does: a stuck partition is
// loud, and a skipped acceptance is a rider who cannot message their driver.
func (l *Lifecycle) handle(ctx context.Context, record *kgo.Record) {
	ctx, span := tracing.Consume(ctx, record, "chat.trip_fact")
	defer span.End()

	var fact wire.TripFact
	if err := json.Unmarshal(record.Value, &fact); err != nil || !fact.Valid() {
		if err == nil {
			err = errors.New("invalid trip fact")
		}
		tracing.Fail(span, err)
		l.skipped("undecodable")
		return
	}
	span.SetName("chat." + fact.Tag)

	trip, relevant := TripOf(fact)
	if !relevant {
		l.skipped("irrelevant")
		return
	}

	wait := 250 * time.Millisecond
	for {
		err := l.chat.TripChanged(ctx, trip)
		if err == nil {
			if l.hooks.OnApplied != nil {
				l.hooks.OnApplied(fact.Tag)
			}
			return
		}

		tracing.Fail(span, err)
		if l.hooks.OnRetry != nil {
			l.hooks.OnRetry()
		}
		slog.Warn("trip fact failed; retrying", "trip", fact.TripID, "tag", fact.Tag, "in", wait, "error", err)

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, 30*time.Second)
	}
}

func (l *Lifecycle) skipped(reason string) {
	if l.hooks.OnSkipped != nil {
		l.hooks.OnSkipped(reason)
	}
}
