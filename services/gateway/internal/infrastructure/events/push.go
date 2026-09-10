// Package events is the gateway's inbound half from the bus: messages addressed
// to a person, delivered to whichever connection that person holds.
package events

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/ishakdeveloper/surge/shared/tracing"
	"github.com/ishakdeveloper/surge/shared/wire"
	"github.com/twmb/franz-go/pkg/kgo"
)

// Pusher is whatever can deliver to a connected user.
type Pusher interface {
	Push(userID string, message []byte) bool
}

// Hooks are the observability seams.
type Hooks struct {
	OnDelivered func()
	OnNotHere   func()
	OnMalformed func()
}

// Consumer routes `ws.push` to connections.
//
// Every gateway instance consumes the whole topic and drops what is not for its
// own connections. That is deliberately the simple choice: the alternative is a
// registry lookup in Redis on the producing side plus per-instance topics, and
// at this message rate — offers, not pings — the wasted reads cost less than
// the coordination would.
//
// It becomes the wrong choice when push volume approaches ping volume. The
// metric that would say so is the ratio of not_here to delivered.
type Consumer struct {
	client *kgo.Client
	pusher Pusher
	hooks  Hooks
}

func NewConsumer(brokers []string, group string, pusher Pusher, hooks Hooks) (*Consumer, error) {
	// Each instance needs every message, so each gets its own group. A shared
	// group would give each partition to exactly one gateway, and an offer
	// would reach the instance that happened to own that partition rather than
	// the one holding the driver's socket.
	client, err := kafkax.NewConsumerGroup(brokers, group, []string{kafkax.TopicWSPush},
		// An offer produced while this instance was down has almost certainly
		// expired; replaying it would dispatch a stale ride.
		kgo.ConsumeResetOffset(kgo.NewOffset().AtEnd()))
	if err != nil {
		return nil, err
	}
	return &Consumer{client: client, pusher: pusher, hooks: hooks}, nil
}

func (c *Consumer) Close() { c.client.Close() }

func (c *Consumer) Run(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return nil
		}

		fetches := c.client.PollRecords(ctx, 2_000)
		if fetches.IsClientClosed() {
			return nil
		}

		fetches.EachRecord(func(record *kgo.Record) {
			_, span := tracing.Consume(ctx, record, "gateway.push")
			defer span.End()

			var offer wire.Offer
			if err := json.Unmarshal(record.Value, &offer); err != nil {
				tracing.Fail(span, err)
				if c.hooks.OnMalformed != nil {
					c.hooks.OnMalformed()
				}
				return
			}

			envelope, err := json.Marshal(wire.ServerMessage{Tag: wire.TagOffer, Offer: &offer})
			if err != nil {
				if c.hooks.OnMalformed != nil {
					c.hooks.OnMalformed()
				}
				return
			}

			if c.pusher.Push(offer.DriverID, envelope) {
				if c.hooks.OnDelivered != nil {
					c.hooks.OnDelivered()
				}
				return
			}

			// Not connected here. Normal, and not worth logging per message —
			// with N gateways, (N-1)/N of every offer lands on an instance that
			// does not hold that driver.
			if c.hooks.OnNotHere != nil {
				c.hooks.OnNotHere()
			}
		})

		if err := c.client.CommitUncommittedOffsets(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("gateway push commit failed", "error", err)
		}
	}
}
