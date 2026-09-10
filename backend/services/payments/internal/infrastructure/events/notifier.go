package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/ishakdeveloper/surge/shared/tracing"
	"github.com/ishakdeveloper/surge/shared/wire"
	"github.com/twmb/franz-go/pkg/kgo"
)

// Notifier pushes PaymentsChanged to a person over `ws.push`, keyed by their
// user id, which every gateway consumes and delivers to whichever socket that
// person holds — the path the trip service's updates already take.
//
// Best effort, deliberately, as those are: the change is committed before
// this runs, and a push to someone who has closed the tab must not fail it.
type Notifier struct{ producer *kgo.Client }

func NewNotifier(producer *kgo.Client) *Notifier { return &Notifier{producer: producer} }

func (n *Notifier) PaymentsChanged(ctx context.Context, userID, tripID string) {
	payload, err := json.Marshal(wire.ServerMessage{
		Tag:      wire.TagPaymentsChanged,
		Payments: &wire.PaymentsChange{TripID: tripID, AtMs: time.Now().UnixMilli()},
	})
	if err != nil {
		slog.Error("payments push could not be encoded", "user", userID, "error", err)
		return
	}

	// Detached from the caller's lifetime: this runs inside a consumer loop or
	// a webhook's request, and a push abandoned because that returned is a
	// rider who never learns their bank wants them.
	tracing.Produce(context.WithoutCancel(ctx), n.producer, &kgo.Record{
		Topic: kafkax.TopicWSPush,
		Key:   []byte(userID),
		Value: payload,
	}, func(_ *kgo.Record, err error) {
		if err != nil {
			slog.Warn("payments push never reached the broker", "user", userID, "error", err)
		}
	})
}
