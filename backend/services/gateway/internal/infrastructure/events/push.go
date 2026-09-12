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
	// Broadcast delivers to everyone connected with a role, and returns how
	// many that reached.
	Broadcast(role string, message []byte) int
}

// Outcome is what became of one push record.
type Outcome int

const (
	Delivered Outcome = iota
	NotHere
	Malformed
)

// Deliver routes one `ws.push` record: to the person its key names, or to a
// role's audience when the key names one (wire.RoleAudience).
//
// A push is a finished ServerMessage keyed by its recipient. The gateway
// decides who receives it and nothing about what it says, which is what lets
// a new kind of push — trip updates, chat, and whatever follows — reach
// clients without a change here.
//
// Decoded only to be checked, then forwarded as the original bytes: a record
// whose tag and payload disagree is dropped here rather than delivered to a
// client whose decoder would reject it.
func Deliver(pusher Pusher, key, value []byte) (Outcome, error) {
	var message wire.ServerMessage
	if err := json.Unmarshal(value, &message); err != nil {
		return Malformed, err
	}
	if !message.Valid() || len(key) == 0 {
		return Malformed, nil
	}

	if role, ok := wire.AudienceRole(string(key)); ok {
		if pusher.Broadcast(role, value) > 0 {
			return Delivered, nil
		}
		return NotHere, nil
	}
	if pusher.Push(string(key), value) {
		return Delivered, nil
	}
	return NotHere, nil
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

func NewConsumer(cluster kafkax.Cluster, group string, pusher Pusher, hooks Hooks) (*Consumer, error) {
	// Each instance needs every message, so each gets its own group. A shared
	// group would give each partition to exactly one gateway, and an offer
	// would reach the instance that happened to own that partition rather than
	// the one holding the driver's socket.
	client, err := kafkax.NewConsumerGroup(cluster, group, []string{kafkax.TopicWSPush},
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

			outcome, err := Deliver(c.pusher, record.Key, record.Value)
			if err != nil {
				tracing.Fail(span, err)
			}
			switch outcome {
			case Delivered:
				if c.hooks.OnDelivered != nil {
					c.hooks.OnDelivered()
				}
			case Malformed:
				if c.hooks.OnMalformed != nil {
					c.hooks.OnMalformed()
				}
			case NotHere:
				// Not connected here. Normal, and not worth logging per
				// message — with N gateways, (N-1)/N of every offer lands on
				// an instance that does not hold that driver.
				if c.hooks.OnNotHere != nil {
					c.hooks.OnNotHere()
				}
			}
		})

		if err := c.client.CommitUncommittedOffsets(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("gateway push commit failed", "error", err)
		}
	}
}
