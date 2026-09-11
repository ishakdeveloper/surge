// Package events is chat's Kafka edge: trip lifecycle facts in, and doorbells
// out on `ws.push`.
package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/ishakdeveloper/surge/services/chat/internal/service"
	"github.com/ishakdeveloper/surge/shared/authz"
	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/ishakdeveloper/surge/shared/tracing"
	"github.com/ishakdeveloper/surge/shared/wire"
	"github.com/twmb/franz-go/pkg/kgo"
)

// Notifier rings chat's doorbells on `ws.push`, keyed by recipient, which
// every gateway instance consumes and delivers to whichever socket that
// person holds.
//
// Best effort, like the trip service's: the message is committed before this
// runs, a push that never arrives is a client that reads it on its next look,
// and a send must not fail because a doorbell did.
type Notifier struct{ producer *kgo.Client }

func NewNotifier(producer *kgo.Client) *Notifier { return &Notifier{producer: producer} }

func (n *Notifier) Changed(ctx context.Context, change service.Change, recipients []string) {
	n.push(ctx, recipients, chatChanged(change))
}

// SupportQueueChanged reaches everyone from support who is connected, through
// the gateway's role audience: the producer cannot know who has the queue
// open, and should not have to.
func (n *Notifier) SupportQueueChanged(ctx context.Context, change service.Change) {
	n.push(ctx, []string{wire.RoleAudience(string(authz.RoleOps))}, chatChanged(change))
}

func (n *Notifier) Read(ctx context.Context, conversationID, userID string, seq int64, at time.Time, recipients []string) {
	n.push(ctx, recipients, wire.ServerMessage{Tag: wire.TagChatRead, ChatRead: &wire.ChatRead{
		ConversationID: conversationID, UserID: userID, Seq: seq, AtMs: at.UnixMilli(),
	}})
}

func (n *Notifier) Typing(ctx context.Context, conversationID, userID string, at time.Time, recipients []string) {
	n.push(ctx, recipients, wire.ServerMessage{Tag: wire.TagChatTyping, ChatTyping: &wire.ChatTyping{
		ConversationID: conversationID, UserID: userID, AtMs: at.UnixMilli(),
	}})
}

func chatChanged(change service.Change) wire.ServerMessage {
	return wire.ServerMessage{Tag: wire.TagChatChanged, Chat: &wire.ChatChange{
		ConversationID: change.ConversationID,
		Kind:           string(change.Kind),
		TripID:         change.TripID,
		LastSeq:        change.LastSeq,
		AtMs:           change.At.UnixMilli(),
	}}
}

func (n *Notifier) push(ctx context.Context, recipients []string, message wire.ServerMessage) {
	if len(recipients) == 0 {
		return
	}
	payload, err := json.Marshal(message)
	if err != nil {
		slog.Error("chat push could not be encoded", "tag", message.Tag, "error", err)
		return
	}

	// Detached, for the trip notifier's reason: this runs at the end of a
	// request, and a doorbell abandoned because the request returned is a
	// message the other side never hears about until they look.
	produceCtx := context.WithoutCancel(ctx)
	for _, recipient := range recipients {
		tracing.Produce(produceCtx, n.producer, &kgo.Record{
			Topic: kafkax.TopicWSPush,
			Key:   []byte(recipient),
			Value: payload,
		}, func(_ *kgo.Record, err error) {
			if err != nil {
				slog.Warn("chat push never reached the broker",
					"tag", message.Tag, "recipient", recipient, "error", err)
			}
		})
	}
}
