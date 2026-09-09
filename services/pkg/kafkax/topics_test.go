package kafkax_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/pkg/kafkax"
)

func brokers(t *testing.T) []string {
	t.Helper()

	value := os.Getenv("KAFKA_BROKERS")
	if value == "" {
		value = "localhost:19092"
	}
	return strings.Split(value, ",")
}

// Provisioning has to be safe to run from every service on every boot, because
// that is exactly how it is called. Running it twice is the test.
func TestEnsureTopicsIsIdempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := kafkax.EnsureTopics(ctx, brokers(t)); err != nil {
		t.Skipf("no broker at %v: %v", brokers(t), err)
	}

	if err := kafkax.EnsureTopics(ctx, brokers(t)); err != nil {
		t.Fatalf("second run should be a no-op, got: %v", err)
	}

	// The two topics a matcher consumes in lockstep must have the same number
	// of partitions, or "owning partition N of geo.events means owning
	// partition N of geo.state" is false and restore-on-assign reads the wrong
	// cells' checkpoints.
	events, err := kafkax.TopicPartitions(ctx, brokers(t), kafkax.TopicGeoEvents)
	if err != nil {
		t.Fatalf("partitions for %s: %v", kafkax.TopicGeoEvents, err)
	}
	state, err := kafkax.TopicPartitions(ctx, brokers(t), kafkax.TopicGeoState)
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
