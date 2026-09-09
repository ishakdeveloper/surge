// Package kafkax is the Kafka vocabulary and client setup this system shares.
//
// The topic layout is the architecture written down, so it lives here rather
// than being spelled out at each call site.
package kafkax

import (
	"context"
	"errors"
	"fmt"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

// Topic names follow the starter's convention, which was the one good idea in
// its otherwise unwritten backend: `<domain>.event.<past tense>` for facts and
// `<domain>.cmd.<imperative>` for commands. Here the split is by stream rather
// than by message, because a stream carries both.
const (
	// TopicLocPing is raw GPS, keyed by driver id so one driver's pings stay
	// ordered. High volume and nothing else: 40k drivers at 4s is 10k/sec.
	TopicLocPing = "loc.ping"

	// TopicGeoEvents is the one that matters.
	//
	// A single topic carrying a tagged union of everything a matcher shard
	// needs — driver movement, ride requests, reservations, replies — keyed by
	// resolution-7 cell.
	//
	// One topic rather than four, because no Kafka consumer-group balancer
	// guarantees co-partitioned assignment across topics: an instance can be
	// given partition 3 of one and partition 5 of another. With a single topic,
	// owning a partition means owning every event for those cells, in total
	// order, which is what lets a shard be one goroutine over in-memory state
	// with no locks at all.
	TopicGeoEvents = "geo.events"

	// TopicGeoState is the shard changelog: compacted, keyed by the same
	// resolution-7 cell, with the same partition count as geo.events so that
	// owning partition N of one means owning partition N of the other.
	//
	// It holds only what cannot be rebuilt — open offers and reservations.
	// Driver positions are deliberately absent: they re-arrive within one ping
	// interval, so checkpointing them would buy four seconds at the cost of
	// writing 10k records a second.
	TopicGeoState = "geo.state"

	// TopicTripEvents is the trip lifecycle, keyed by trip id.
	TopicTripEvents = "trip.events"

	// TopicWSPush is server-to-client delivery, keyed by user id.
	TopicWSPush = "ws.push"
)

// GeoPartitions is the shard count, and therefore the ceiling on matcher
// parallelism.
//
// 64 against roughly 128 resolution-7 cells over Amsterdam: about two cells per
// partition, so adding an instance moves a meaningful slice of the map without
// any single partition owning a quarter of the city.
const GeoPartitions = 32

type topicSpec struct {
	name       string
	partitions int32
	configs    map[string]*string
}

func stringPtr(s string) *string { return &s }

func specs() []topicSpec {
	compact := stringPtr("compact")
	// A day of raw pings is far more than any replay needs and is the bulk of
	// the disk this system uses.
	sixHours := stringPtr("21600000")

	return []topicSpec{
		{TopicLocPing, 32, map[string]*string{"retention.ms": sixHours}},
		{TopicGeoEvents, GeoPartitions, map[string]*string{"retention.ms": sixHours}},
		{TopicGeoState, GeoPartitions, map[string]*string{
			"cleanup.policy": compact,
			// Compact aggressively: a restoring shard wants the latest record
			// per cell, and a long tail of superseded ones only slows it down.
			"min.cleanable.dirty.ratio": stringPtr("0.1"),
			"segment.ms":                stringPtr("60000"),
		}},
		{TopicTripEvents, 16, nil},
		{TopicWSPush, 16, map[string]*string{"retention.ms": stringPtr("600000")}},
	}
}

// EnsureTopics creates any topic that does not exist, and leaves existing ones
// alone. Safe to call from every service on boot.
//
// Replication factor 1 because this is a single-broker development cluster;
// anything else fails to create rather than silently degrading.
func EnsureTopics(ctx context.Context, brokers []string) error {
	client, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		return fmt.Errorf("kafkax: client: %w", err)
	}
	defer client.Close()

	admin := kadm.NewClient(client)

	for _, spec := range specs() {
		responses, err := admin.CreateTopics(ctx, spec.partitions, 1, spec.configs, spec.name)
		if err != nil {
			return fmt.Errorf("kafkax: create %s: %w", spec.name, err)
		}

		for _, response := range responses {
			// kadm reports "already exists" per topic rather than as a call
			// failure, and it is the normal case on every boot after the first.
			if response.Err != nil && !errors.Is(response.Err, kerr.TopicAlreadyExists) {
				return fmt.Errorf("kafkax: create %s: %w", response.Topic, response.Err)
			}
		}
	}

	return nil
}

// TopicPartitions reports the live partition count for a topic, which the
// matcher logs at startup — a partition count that drifted from GeoPartitions
// silently changes how many shards exist.
func TopicPartitions(ctx context.Context, brokers []string, topic string) (int, error) {
	client, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		return 0, fmt.Errorf("kafkax: client: %w", err)
	}
	defer client.Close()

	metadata, err := kadm.NewClient(client).Metadata(ctx, topic)
	if err != nil {
		return 0, fmt.Errorf("kafkax: metadata for %s: %w", topic, err)
	}

	details, ok := metadata.Topics[topic]
	if !ok {
		return 0, fmt.Errorf("kafkax: topic %s does not exist", topic)
	}

	return len(details.Partitions), nil
}
