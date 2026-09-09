package kafkax

import (
	"fmt"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

// NewProducer builds a client tuned for throughput over latency.
//
// Linger is the whole trick at this scale. 10,000 pings a second as 10,000
// individual produce requests is a syscall storm; 5ms of linger batches them
// into roughly 200 requests a second carrying 50 records each, at a cost of
// 5ms of added delay on a pipeline whose ping interval is 4,000ms.
func NewProducer(brokers []string, options ...kgo.Opt) (*kgo.Client, error) {
	defaults := []kgo.Opt{
		kgo.SeedBrokers(brokers...),
		kgo.ProducerLinger(5 * time.Millisecond),
		kgo.ProducerBatchMaxBytes(1 << 20),
		// Idempotent by default (franz-go's default), so a retry after a
		// timeout cannot duplicate a record. That matters most on geo.events,
		// where a duplicate reservation would be a real double-booking.
		kgo.RecordRetries(10),
		kgo.MaxBufferedRecords(1 << 18),
		kgo.RequiredAcks(kgo.AllISRAcks()),
	}

	client, err := kgo.NewClient(append(defaults, options...)...)
	if err != nil {
		return nil, fmt.Errorf("kafkax: producer: %w", err)
	}
	return client, nil
}

// NewConsumerGroup builds a client for a co-operatively balanced group.
//
// Cooperative sticky, not eager: an eager rebalance revokes every partition
// from every member and reassigns from scratch, which for the matcher means the
// entire city stops matching while one instance joins. Cooperative revokes only
// the partitions that actually move, so adding a fourth matcher to three stalls
// a quarter of the map rather than all of it.
//
// Auto-commit is off. A shard's offset must not advance until its state has
// been checkpointed, or a crash silently drops the events between the last
// commit and the last checkpoint.
func NewConsumerGroup(brokers []string, group string, topics []string, options ...kgo.Opt) (*kgo.Client, error) {
	defaults := []kgo.Opt{
		kgo.SeedBrokers(brokers...),
		kgo.ConsumerGroup(group),
		kgo.ConsumeTopics(topics...),
		kgo.Balancers(kgo.CooperativeStickyBalancer()),
		kgo.DisableAutoCommit(),
		kgo.FetchMaxWait(100 * time.Millisecond),
		// A shard that falls behind should catch up in large fetches rather
		// than in a long series of small ones.
		kgo.FetchMaxBytes(50 << 20),
		// Long enough to survive a GC pause under load, short enough that a
		// killed instance's partitions move within a few seconds — which is
		// what `make chaos-kill` measures.
		kgo.SessionTimeout(10 * time.Second),
		kgo.HeartbeatInterval(3 * time.Second),
	}

	client, err := kgo.NewClient(append(defaults, options...)...)
	if err != nil {
		return nil, fmt.Errorf("kafkax: consumer group %s: %w", group, err)
	}
	return client, nil
}
