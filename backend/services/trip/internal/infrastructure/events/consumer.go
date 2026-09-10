package events

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/ishakdeveloper/surge/services/trip/internal/domain"
	"github.com/ishakdeveloper/surge/services/trip/internal/service"
	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/ishakdeveloper/surge/shared/tracing"
	"github.com/ishakdeveloper/surge/shared/wire"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel/trace"
)

// Hooks are the observability seams.
type Hooks struct {
	OnMatched   func()
	OnUnmatched func()
	OnRejected  func(reason string)
}

// Consumer applies matcher outcomes to trip state.
type Consumer struct {
	client  *kgo.Client
	service *service.Service
	hooks   Hooks
}

func NewConsumer(brokers []string, group string, trips *service.Service, hooks Hooks) (*Consumer, error) {
	// Unlike positions, trip outcomes are worth replaying: a trip service that
	// was down for a minute must still learn that a driver accepted, or the
	// rider is left looking at a spinner for a ride that is already on its way.
	// So this one starts at the beginning of what it has not committed.
	client, err := kafkax.NewConsumerGroup(brokers, group, []string{kafkax.TopicTripEvents})
	if err != nil {
		return nil, err
	}
	return &Consumer{client: client, service: trips, hooks: hooks}, nil
}

func (c *Consumer) Close() { c.client.Close() }

func (c *Consumer) Run(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return nil
		}

		fetches := c.client.PollRecords(ctx, 500)
		if fetches.IsClientClosed() {
			return nil
		}

		fetches.EachRecord(func(record *kgo.Record) {
			recordCtx, span := tracing.Consume(ctx, record, "trip.event")
			defer span.End()

			var event wire.TripEvent
			if err := json.Unmarshal(record.Value, &event); err != nil {
				tracing.Fail(span, err)
				return
			}

			span.SetName("trip." + event.Tag)
			c.apply(recordCtx, event, span)
		})

		if err := c.client.CommitUncommittedOffsets(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("trip consumer commit failed", "error", err)
		}
	}
}

func (c *Consumer) apply(ctx context.Context, event wire.TripEvent, span trace.Span) {
	var err error

	switch event.Tag {
	case wire.TagTripMatched:
		if event.Matched == nil {
			return
		}
		_, err = c.service.Matched(ctx, event.TripID, event.Matched.DriverID)
		if err == nil && c.hooks.OnMatched != nil {
			c.hooks.OnMatched()
		}

	case wire.TagTripUnmatched:
		_, err = c.service.Unmatched(ctx, event.TripID)
		if err == nil && c.hooks.OnUnmatched != nil {
			c.hooks.OnUnmatched()
		}

	default:
		return
	}

	switch {
	case err == nil:
		return

	case errors.Is(err, domain.ErrInvalidTransition):
		// The common and uninteresting case. A cancelled trip being told it was
		// matched, or a redelivered event arriving after the trip moved on:
		// the transition is refused, which is the transition doing its job.
		if c.hooks.OnRejected != nil {
			c.hooks.OnRejected("invalid_transition")
		}

	case errors.Is(err, domain.ErrNotFound):
		// A trip event for a trip that does not exist. Possible when the
		// matcher is fed requests by something other than this service — the
		// simulator does exactly that — so it is expected rather than alarming.
		if c.hooks.OnRejected != nil {
			c.hooks.OnRejected("unknown_trip")
		}

	default:
		tracing.Fail(span, err)
		slog.Warn("trip event failed", "trip", event.TripID, "tag", event.Tag, "error", err)
		if c.hooks.OnRejected != nil {
			c.hooks.OnRejected("error")
		}
	}
}
