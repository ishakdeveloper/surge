// Package events is ingest's Kafka edge: it consumes raw pings and produces
// the cell-addressed events the matcher shards consume.
//
// The re-keying happens here rather than in the domain because it is a
// transport concern: the domain knows about drivers and cells, and nothing
// about partitions or topics.
package events

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/ishakdeveloper/surge/services/ingest/internal/domain"
	"github.com/ishakdeveloper/surge/services/ingest/internal/service"
	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/ishakdeveloper/surge/shared/tracing"
	"github.com/ishakdeveloper/surge/shared/wire"
	"github.com/twmb/franz-go/pkg/kgo"
)

// Hooks are the observability seams, so this package does not import Prometheus.
type Hooks struct {
	OnIndexed       func()
	OnRejected      func(reason string)
	OnStale         func(duplicate bool)
	OnIndexChanged  func()
	OnShardHandover func()
	OnProduced      func(tag string)
	OnProduceError  func()
	OnAge           func(seconds float64)
	OnProcess       func(seconds float64)
	OnSnapshot      func(domain.Stats)
}

// Consumer runs the ingest loop.
type Consumer struct {
	client   *kgo.Client
	producer *kgo.Client
	index    *domain.Index
	hooks    Hooks
}

func NewConsumer(client, producer *kgo.Client, index *domain.Index, hooks Hooks) *Consumer {
	return &Consumer{client: client, producer: producer, index: index, hooks: hooks}
}

func (c *Consumer) Run(ctx context.Context) error {
	// Offsets are committed on a timer rather than per record. Per-record
	// commits at 10k/sec would be 10k commit requests a second — more traffic
	// than the pings themselves — and the cost of a crash here is replaying a
	// few seconds of positions that are about to be overwritten anyway.
	commit := time.NewTicker(5 * time.Second)
	defer commit.Stop()

	// Gauges are sampled on a timer, NOT once per poll.
	//
	// They used to be updated at the bottom of the poll loop, and that was the
	// first thing to break under load: Stats() walks every driver to count
	// distinct shards, so at 20,000 drivers and a few hundred polls a second it
	// was several million map operations a second spent entirely on telemetry.
	// End-to-end ping age went from 25ms to over 30 seconds, the consumer
	// missed its session timeout, and the resulting rebalance replayed records
	// — which showed up as thousands of "stale" pings and looked like an
	// ordering bug rather than an observer effect.
	sample := time.NewTicker(time.Second)
	defer sample.Stop()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-commit.C:
				if err := c.client.CommitUncommittedOffsets(ctx); err != nil {
					slog.Warn("commit failed", "error", err)
				}
			case <-sample.C:
				if c.hooks.OnSnapshot != nil {
					c.hooks.OnSnapshot(c.index.Stats())
				}
			}
		}
	}()

	for {
		if err := ctx.Err(); err != nil {
			return nil
		}

		fetches := c.client.PollRecords(ctx, 10_000)
		if fetches.IsClientClosed() {
			return nil
		}

		fetches.EachError(func(topic string, partition int32, err error) {
			if !errors.Is(err, context.Canceled) {
				slog.Warn("fetch error", "topic", topic, "partition", partition, "error", err)
			}
		})

		fetches.EachRecord(func(record *kgo.Record) {
			// The span the producer opened continues here, across the broker.
			recordCtx, span := tracing.Consume(ctx, record, "ingest.ping")
			defer span.End()

			var ping wire.DriverPing
			if err := json.Unmarshal(record.Value, &ping); err != nil {
				c.reject("malformed")
				return
			}
			if !ping.Valid() {
				c.reject("invalid")
				return
			}

			// Measured from when the driver said it sent, not from the Kafka
			// timestamp: the whole point is the age of the map as a rider would
			// experience it, which includes time spent in the broker.
			age := time.Since(time.UnixMilli(ping.SentAtMs)).Seconds()
			if c.hooks.OnAge != nil {
				c.hooks.OnAge(age)
			}

			started := time.Now()
			observation, err := c.index.Observe(ping)
			if c.hooks.OnProcess != nil {
				c.hooks.OnProcess(time.Since(started).Seconds())
			}

			if err != nil {
				c.reject("uncellable")
				return
			}

			if observation.Stale {
				if c.hooks.OnStale != nil {
					c.hooks.OnStale(observation.Duplicate)
				}
				return
			}

			if observation.IndexChanged {
				if c.hooks.OnIndexChanged != nil {
					c.hooks.OnIndexChanged()
				}
			}
			if observation.ShardChanged {
				if c.hooks.OnShardHandover != nil {
					c.hooks.OnShardHandover()
				}
			}

			for _, event := range service.Translate(observation, time.Now()) {
				payload, err := json.Marshal(event)
				if err != nil {
					c.reject("unencodable")
					continue
				}

				// Keyed by shard cell. This single line is the sharding: the
				// broker's partitioner turns a place into an owner, and every
				// event for that place lands in the same partition in order.
				// Traced, so the matcher's span is a child of this one and a
				// single trip reads as one trace rather than four.
				tracing.Produce(recordCtx, c.producer, &kgo.Record{
					Topic: kafkax.TopicGeoEvents,
					Key:   []byte(event.Cell),
					Value: payload,
				}, func(_ *kgo.Record, err error) {
					if err != nil {
						if c.hooks.OnProduceError != nil {
							c.hooks.OnProduceError()
						}
						return
					}
					if c.hooks.OnProduced != nil {
						c.hooks.OnProduced(event.Tag)
					}
				})
			}

			if c.hooks.OnIndexed != nil {
				c.hooks.OnIndexed()
			}
		})
	}
}

func (c *Consumer) reject(reason string) {
	if c.hooks.OnRejected != nil {
		c.hooks.OnRejected(reason)
	}
}
