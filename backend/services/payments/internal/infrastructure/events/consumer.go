// Package events is payments' Kafka edge: trip lifecycle facts in, and payment
// facts out through the outbox the repository writes.
package events

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
	"github.com/ishakdeveloper/surge/services/payments/internal/service"
	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/ishakdeveloper/surge/shared/tracing"
	"github.com/ishakdeveloper/surge/shared/wire"
	"github.com/twmb/franz-go/pkg/kgo"
)

// Hooks are the observability seams.
type Hooks struct {
	// OnApplied counts a fact acted on, by tag.
	OnApplied func(tag string)
	// OnSkipped counts a fact deliberately moved past, by reason: a late
	// message the state machine refused, a trip that was never held, a record
	// that could not be decoded.
	OnSkipped func(reason string)
	// OnRetry counts an attempt that failed transiently and will be retried.
	OnRetry func()
}

// Consumer applies trip lifecycle facts to payments.
type Consumer struct {
	client  *kgo.Client
	service *service.Service
	hooks   Hooks
}

func NewConsumer(brokers []string, group string, payments *service.Service, hooks Hooks) (*Consumer, error) {
	client, err := kafkax.NewConsumerGroup(brokers, group, []string{kafkax.TopicTripLifecycle})
	if err != nil {
		return nil, err
	}
	return &Consumer{client: client, service: payments, hooks: hooks}, nil
}

func (c *Consumer) Close() { c.client.Close() }

func (c *Consumer) Run(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return nil
		}

		fetches := c.client.PollRecords(ctx, 100)
		if fetches.IsClientClosed() {
			return nil
		}

		fetches.EachRecord(func(record *kgo.Record) {
			if ctx.Err() == nil {
				c.handle(ctx, record)
			}
		})

		// Only a batch handled to the end is committed. A shutdown mid-batch
		// leaves the rest uncommitted, and they are redelivered — safely,
		// because everything they cause is idempotent.
		if ctx.Err() != nil {
			return nil
		}
		if err := c.client.CommitUncommittedOffsets(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("payments consumer commit failed", "error", err)
		}
	}
}

// handle applies one fact, retrying in place until it lands or the service is
// shutting down.
//
// Retried rather than skipped, and blocking its partition while it does: a
// stuck partition is loud — lag climbs, an alert fires, somebody looks — and a
// skipped completion is a ride nobody paid for that nobody noticed.
func (c *Consumer) handle(ctx context.Context, record *kgo.Record) {
	ctx, span := tracing.Consume(ctx, record, "payments.trip_fact")
	defer span.End()

	var fact wire.TripFact
	if err := json.Unmarshal(record.Value, &fact); err != nil || !fact.Valid() {
		if err == nil {
			err = errors.New("invalid trip fact")
		}
		tracing.Fail(span, err)
		c.skipped("undecodable")
		return
	}
	span.SetName("payments." + fact.Tag)

	wait := 250 * time.Millisecond
	for {
		err := c.apply(ctx, fact)
		switch {
		case err == nil:
			if c.hooks.OnApplied != nil {
				c.hooks.OnApplied(fact.Tag)
			}
			return
		case errors.Is(err, domain.ErrInvalidTransition):
			// A late or repeated fact the state machine refused: a completion
			// for a trip whose hold was already released, say. The refusal is
			// the answer.
			c.skipped("invalid_transition")
			return
		case errors.Is(err, service.ErrUnheld):
			c.skipped("unheld")
			return
		}

		tracing.Fail(span, err)
		if c.hooks.OnRetry != nil {
			c.hooks.OnRetry()
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

func (c *Consumer) apply(ctx context.Context, fact wire.TripFact) error {
	trip := service.Trip{
		ID: fact.TripID, RiderID: fact.RiderID, DriverID: fact.DriverID,
		TotalCents: fact.TotalCents, Currency: fact.Currency,
	}

	switch fact.Tag {
	case wire.FactTripRequested:
		return c.service.TripRequested(ctx, trip)
	case wire.FactTripCompleted:
		return c.service.TripCompleted(ctx, trip)
	case wire.FactTripCancelled, wire.FactTripUnmatched:
		return c.service.TripEnded(ctx, trip)
	default:
		return nil
	}
}

func (c *Consumer) skipped(reason string) {
	if c.hooks.OnSkipped != nil {
		c.hooks.OnSkipped(reason)
	}
}
