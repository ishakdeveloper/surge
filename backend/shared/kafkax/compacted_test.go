package kafkax_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

// A matcher handed a set of partitions restores all of them in one read, so
// that read has to return the latest value for every key in every partition —
// and an empty partition as present-but-empty rather than missing, or the
// shard for it would wait on a checkpoint that is never coming.
func TestReadCompactedReturnsTheLatestValuePerKeyPerPartition(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cluster := localCluster(t)
	admin, err := cluster.Client()
	if err != nil {
		t.Skipf("no broker at %v: %v", cluster.Brokers, err)
	}
	defer admin.Close()
	topics := kadm.NewClient(admin)

	topic := fmt.Sprintf("surge-test-compacted-%d", time.Now().UnixNano())
	compact := "compact"
	if _, err := topics.CreateTopic(ctx, 3, 1, map[string]*string{"cleanup.policy": &compact}, topic); err != nil {
		t.Skipf("no broker at %v: %v", cluster.Brokers, err)
	}
	t.Cleanup(func() { _, _ = topics.DeleteTopic(context.Background(), topic) })

	producer, err := cluster.Client(kgo.RecordPartitioner(kgo.ManualPartitioner()))
	if err != nil {
		t.Fatalf("producer: %v", err)
	}
	defer producer.Close()

	// cell-a is written twice: a checkpoint superseded by a later one, which is
	// the case a restore exists to get right.
	written := []*kgo.Record{
		{Topic: topic, Partition: 0, Key: []byte("cell-a"), Value: []byte("first")},
		{Topic: topic, Partition: 0, Key: []byte("cell-a"), Value: []byte("second")},
		{Topic: topic, Partition: 0, Key: []byte("cell-b"), Value: []byte("only")},
		{Topic: topic, Partition: 1, Key: []byte("cell-c"), Value: []byte("one")},
	}
	if err := producer.ProduceSync(ctx, written...).FirstErr(); err != nil {
		t.Fatalf("produce: %v", err)
	}

	got, err := kafkax.ReadCompacted(ctx, cluster, topic, []int32{0, 1, 2})
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	want := map[int32]map[string]string{
		0: {"cell-a": "second", "cell-b": "only"},
		1: {"cell-c": "one"},
		2: {},
	}
	for partition, keys := range want {
		values, present := got[partition]
		if !present {
			t.Errorf("partition %d is missing from the result rather than empty", partition)
			continue
		}
		if len(values) != len(keys) {
			t.Errorf("partition %d: %d keys, want %d", partition, len(values), len(keys))
		}
		for key, value := range keys {
			if string(values[key]) != value {
				t.Errorf("partition %d, %s = %q, want %q", partition, key, values[key], value)
			}
		}
	}
}
