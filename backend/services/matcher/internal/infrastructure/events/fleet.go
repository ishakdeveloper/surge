package events

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/ishakdeveloper/surge/shared/wire"
	"github.com/twmb/franz-go/pkg/kgo"
)

// Roster is the matcher's view of who the fleet has approved, from the
// compacted `fleet.drivers` topic.
//
// Read by every instance without a consumer group, because every shard needs
// every driver: a partition of `geo.events` holds whichever drivers happen to
// be standing in its cells, and nothing co-partitions the two topics. Sixteen
// partitions of standings against thirty-two of geography would hand one
// instance the approvals for drivers another instance is dispatching.
//
// It lives beside the Runner rather than inside a Shard for the same reason.
// A shard is rebuilt from pings after every rebalance and is not allowed to
// outlive its partition; the approvals are neither, and rereading them
// thirty-two times would be thirty-two clients.
type Roster struct {
	client *kgo.Client

	mu      sync.RWMutex
	drivers map[string]wire.FleetFact
}

// NewRoster loads the current standing of every driver, then follows changes.
//
// The snapshot is taken before this returns, and that ordering is the whole
// point: a matcher that started gating before it had read the topic would
// withdraw every approved driver in the city for as long as the first fetch
// took.
func NewRoster(ctx context.Context, cluster kafkax.Cluster) (*Roster, error) {
	count, err := kafkax.TopicPartitions(ctx, cluster, kafkax.TopicFleetDrivers)
	if err != nil {
		return nil, fmt.Errorf("matcher: fleet roster: %w", err)
	}

	partitions := make([]int32, 0, count)
	for partition := range int32(count) {
		partitions = append(partitions, partition)
	}

	records, err := kafkax.ReadCompacted(ctx, cluster, kafkax.TopicFleetDrivers, partitions)
	if err != nil {
		return nil, fmt.Errorf("matcher: fleet roster snapshot: %w", err)
	}

	roster := NewMemoryRoster()
	for _, keyed := range records {
		for key, value := range keyed {
			roster.record([]byte(key), value)
		}
	}

	client, err := cluster.Client(
		kgo.ConsumeTopics(kafkax.TopicFleetDrivers),
		// From the start again, rather than from where the snapshot ended: a
		// compacted topic read twice is the same standings twice, and Record
		// keeps the newer either way.
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	if err != nil {
		return nil, fmt.Errorf("matcher: fleet roster reader: %w", err)
	}
	roster.client = client
	return roster, nil
}

// NewMemoryRoster is a roster with no topic behind it, fed through Record.
func NewMemoryRoster() *Roster {
	return &Roster{drivers: map[string]wire.FleetFact{}}
}

func (r *Roster) Close() {
	if r.client != nil {
		r.client.Close()
	}
}

func (r *Roster) Run(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		fetches := r.client.PollRecords(ctx, 1000)
		if fetches.IsClientClosed() {
			return nil
		}
		fetches.EachRecord(func(record *kgo.Record) { r.record(record.Key, record.Value) })
	}
}

func (r *Roster) record(key, value []byte) {
	if len(value) == 0 {
		// A tombstone: the fleet has forgotten this driver. Forgetting is not
		// approving, so they come off the roster rather than keeping the
		// standing of a record that no longer exists.
		r.forget(string(key))
		return
	}

	var fact wire.FleetFact
	if err := json.Unmarshal(value, &fact); err != nil || !fact.Valid() {
		return
	}
	r.Record(fact)
}

func (r *Roster) forget(driverID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.drivers, driverID)
}

// Record keeps the newest standing for a driver. Reading a compacted topic
// from the start can deliver an older record after a newer one across a
// replay, and the timestamp is what decides.
func (r *Roster) Record(fact wire.FleetFact) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if current, ok := r.drivers[fact.DriverID]; ok && current.AtMs > fact.AtMs {
		return
	}
	r.drivers[fact.DriverID] = fact
}

// Approved is whether this driver may be offered work.
//
// A driver nobody has said anything about is not approved. That is the point
// of gating at all: the fleet publishes an approval the moment it derives one,
// so silence is somebody who never finished onboarding rather than somebody
// whose paperwork went unmentioned.
func (r *Roster) Approved(driverID string) bool {
	r.mu.RLock()
	fact, ok := r.drivers[driverID]
	r.mu.RUnlock()
	return ok && fact.Tag == wire.FactDriverApproved
}

// Size is how many drivers the roster has heard about, approved or not.
func (r *Roster) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.drivers)
}
