package kafkax

import (
	"context"
	"fmt"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
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

// NewShardConsumer is the matcher's consumer.
//
// Differs from NewConsumerGroup in one way that matters: offsets are committed
// only for records explicitly marked as processed. A shard's offset must not
// advance until its state has been checkpointed, or a crash silently drops
// every event between the last commit and the last checkpoint — and those are
// exactly the events that were creating reservations.
func NewShardConsumer(brokers []string, group string, topics []string, options ...kgo.Opt) (*kgo.Client, error) {
	defaults := []kgo.Opt{
		kgo.SeedBrokers(brokers...),
		kgo.ConsumerGroup(group),
		kgo.ConsumeTopics(topics...),
		// Cooperative, not eager. An eager rebalance revokes every partition
		// from every member and reassigns from scratch, so adding a fourth
		// matcher to three would stop the entire city from matching.
		// Cooperative moves only the partitions that actually change hands.
		kgo.Balancers(kgo.CooperativeStickyBalancer()),
		kgo.AutoCommitMarks(),
		kgo.AutoCommitInterval(5 * time.Second),
		kgo.FetchMaxWait(100 * time.Millisecond),
		kgo.FetchMaxBytes(50 << 20),
		// Long enough to survive a GC pause under load, short enough that a
		// killed instance's partitions move within a few seconds — which is
		// what the chaos target measures.
		kgo.SessionTimeout(10 * time.Second),
		kgo.HeartbeatInterval(3 * time.Second),
		// Shards start from the end: a position is stale in seconds, and
		// reservations are recovered from the compacted checkpoint instead.
		kgo.ConsumeResetOffset(kgo.NewOffset().AtEnd()),
	}

	client, err := kgo.NewClient(append(defaults, options...)...)
	if err != nil {
		return nil, fmt.Errorf("kafkax: shard consumer %s: %w", group, err)
	}
	return client, nil
}

// ReadCompacted reads partitions of a compacted topic to their ends and returns
// the surviving value for each key, per partition.
//
// One end-offset lookup and one reader for all of them. It replaced a
// per-partition version that opened two new clients and looked up offsets for
// the whole topic every time, and that difference is the reason it exists: the
// matcher restores every partition it is handed through here, and thirty-two of
// those against a slow broker took long enough to lose the consumer-group
// session and be handed them all over again.
//
// Every requested partition is present in the result, empty if it held nothing.
func ReadCompacted(ctx context.Context, brokers []string, topic string, partitions []int32) (map[int32]map[string][]byte, error) {
	latest := make(map[int32]map[string][]byte, len(partitions))
	for _, partition := range partitions {
		latest[partition] = map[string][]byte{}
	}
	if len(partitions) == 0 {
		return latest, nil
	}

	admin, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		return nil, fmt.Errorf("kafkax: admin client: %w", err)
	}
	ends, err := kadm.NewClient(admin).ListEndOffsets(ctx, topic)
	admin.Close()
	if err != nil {
		return nil, fmt.Errorf("kafkax: end offsets for %s: %w", topic, err)
	}

	// Only partitions with something in them are read; an empty one is done.
	remaining := make(map[int32]int64)
	start := make(map[int32]kgo.Offset)
	for _, partition := range partitions {
		end, ok := ends.Lookup(topic, partition)
		if !ok || end.Offset <= 0 {
			continue
		}
		remaining[partition] = end.Offset
		start[partition] = kgo.NewOffset().AtStart()
	}
	if len(remaining) == 0 {
		return latest, nil
	}

	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{topic: start}),
	)
	if err != nil {
		return nil, fmt.Errorf("kafkax: checkpoint reader: %w", err)
	}
	defer client.Close()

	for len(remaining) > 0 {
		fetches := client.PollFetches(ctx)
		if fetches.IsClientClosed() || ctx.Err() != nil {
			return latest, ctx.Err()
		}
		if err := fetches.Err0(); err != nil {
			return nil, fmt.Errorf("kafkax: reading %s: %w", topic, err)
		}

		fetches.EachRecord(func(record *kgo.Record) {
			values, wanted := latest[record.Partition]
			if !wanted {
				return
			}
			// Later records supersede earlier ones for the same key, which is
			// what compaction guarantees and what makes this a state read
			// rather than a log replay.
			values[string(record.Key)] = record.Value
			if end, open := remaining[record.Partition]; open && record.Offset >= end-1 {
				delete(remaining, record.Partition)
			}
		})
	}

	return latest, nil
}
