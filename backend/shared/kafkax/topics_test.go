package kafkax_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/shared/kafkax"
)

// localCluster is the broker the integration tests run against: KAFKA_BROKERS,
// or the compose one. Always one replica, which a single broker can satisfy.
func localCluster(t *testing.T) kafkax.Cluster {
	t.Helper()

	value := os.Getenv("KAFKA_BROKERS")
	if value == "" {
		value = "localhost:19092"
	}
	return kafkax.Cluster{Brokers: strings.Split(value, ","), Replication: 1}
}

// Provisioning has to be safe to run from every service on every boot, because
// that is exactly how it is called. Running it twice is the test.
func TestEnsureTopicsIsIdempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cluster := localCluster(t)
	if err := kafkax.EnsureTopics(ctx, cluster); err != nil {
		t.Skipf("no broker at %v: %v", cluster.Brokers, err)
	}

	if err := kafkax.EnsureTopics(ctx, cluster); err != nil {
		t.Fatalf("second run should be a no-op, got: %v", err)
	}

	// The two topics a matcher consumes in lockstep must have the same number
	// of partitions, or "owning partition N of geo.events means owning
	// partition N of geo.state" is false and restore-on-assign reads the wrong
	// cells' checkpoints.
	events, err := kafkax.TopicPartitions(ctx, cluster, kafkax.TopicGeoEvents)
	if err != nil {
		t.Fatalf("partitions for %s: %v", kafkax.TopicGeoEvents, err)
	}
	state, err := kafkax.TopicPartitions(ctx, cluster, kafkax.TopicGeoState)
	if err != nil {
		t.Fatalf("partitions for %s: %v", kafkax.TopicGeoState, err)
	}

	if events != state {
		t.Errorf("%s has %d partitions and %s has %d; they must be co-partitioned",
			kafkax.TopicGeoEvents, events, kafkax.TopicGeoState, state)
	}
	if events != kafkax.GeoPartitions {
		t.Errorf("%s has %d partitions, expected %d", kafkax.TopicGeoEvents, events, kafkax.GeoPartitions)
	}
}
