package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/ishakdeveloper/surge/services/gateway/internal/domain"
	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/ishakdeveloper/surge/shared/wire"
	"github.com/twmb/franz-go/pkg/kgo"
)

// FleetConsumer keeps the gateway's picture of the city current from the
// matchers' frames.
//
// Its own group per instance, like the push consumer: every gateway needs every
// frame, because a console may be connected to any of them.
type FleetConsumer struct {
	client *kgo.Client
	fleet  *domain.Fleet
}

func NewFleetConsumer(cluster kafkax.Cluster, group string, fleet *domain.Fleet) (*FleetConsumer, error) {
	client, err := kafkax.NewConsumerGroup(cluster, group, []string{kafkax.TopicFleetFrames},
		// A frame is superseded a second after it is written. A gateway
		// starting up wants the next one, not the backlog.
		kgo.ConsumeResetOffset(kgo.NewOffset().AtEnd()))
	if err != nil {
		return nil, err
	}
	return &FleetConsumer{client: client, fleet: fleet}, nil
}

func (c *FleetConsumer) Close() { c.client.Close() }

func (c *FleetConsumer) Run(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return nil
		}

		fetches := c.client.PollRecords(ctx, 500)
		if fetches.IsClientClosed() {
			return nil
		}

		now := time.Now()
		fetches.EachRecord(func(record *kgo.Record) {
			var frame wire.FleetFrame
			if err := json.Unmarshal(record.Value, &frame); err != nil || frame.Tag != wire.TagFleetFrame {
				return
			}
			c.fleet.Record(frame, now)
		})

		if err := c.client.CommitUncommittedOffsets(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("gateway fleet commit failed", "error", err)
		}
	}
}
