package events

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ishakdeveloper/surge/services/gateway/internal/domain"
	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/ishakdeveloper/surge/shared/wire"
	"github.com/twmb/franz-go/pkg/kgo"
)

// PickupConsumer teaches the ETA model from the pickups the fleet made.
//
// No consumer group, and from the start of the topic's day of retention, on
// every instance: each gateway predicts for its own riders, and one that
// starts mid-afternoon should already know the morning's traffic.
type PickupConsumer struct {
	client *kgo.Client
	model  *domain.EtaModel
	// onScored is told how the model did on each pickup observed after this
	// instance started. The replayed history trains the model but is not
	// scored again, or every restart would count the day twice.
	onScored func(learnedError, naiveError float64)
	started  time.Time
}

func NewPickupConsumer(brokers []string, model *domain.EtaModel, onScored func(learnedError, naiveError float64)) (*PickupConsumer, error) {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ConsumeTopics(kafkax.TopicPickupsObserved),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	if err != nil {
		return nil, fmt.Errorf("gateway: pickup reader: %w", err)
	}
	return &PickupConsumer{client: client, model: model, onScored: onScored, started: time.Now()}, nil
}

func (c *PickupConsumer) Close() { c.client.Close() }

func (c *PickupConsumer) Run(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		fetches := c.client.PollRecords(ctx, 1000)
		if fetches.IsClientClosed() {
			return nil
		}
		fetches.EachRecord(func(record *kgo.Record) {
			var pickup wire.PickupObserved
			if err := json.Unmarshal(record.Value, &pickup); err != nil || pickup.Tag != wire.TagPickupObserved {
				return
			}
			learned, naive, scored := c.model.Observe(pickup)
			if scored && c.onScored != nil && time.UnixMilli(pickup.AtMs).After(c.started) {
				c.onScored(learned, naive)
			}
		})
	}
}
