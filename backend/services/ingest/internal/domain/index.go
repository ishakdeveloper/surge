// Package ingest consumes GPS pings and maintains the live position index.
//
// The point of Phase 1's version is to prove one claim: at 10,000 writes a
// second, a database round trip per ping is absurd and the position index has
// to be in memory. Everything here is the shape that claim implies.
package domain

import (
	"sync"

	"github.com/ishakdeveloper/surge/shared/geo"
	"github.com/ishakdeveloper/surge/shared/wire"
)

// Entry is a driver's last known position.
type Entry struct {
	DriverID string
	Point    geo.Point
	Heading  float64
	Status   wire.DriverStatus
	Epoch    uint64
	Seq      uint64
	Index    geo.Cell
	Shard    geo.Cell
	// SentAtMs is carried through from the original ping so that end-to-end
	// latency stays measurable across the re-keying hop rather than restarting
	// its clock here.
	SentAtMs int64

	// EnRouteSinceMs and EnRouteFrom are when and where the driver set off for
	// a pickup, carried from ping to ping while they stay enroute_pickup.
	EnRouteSinceMs int64
	EnRouteFrom    geo.Point
}

// Index is the in-memory geospatial state.
//
// Sharded by index cell rather than one big map behind one mutex: at 10k
// writes/sec a single lock is the bottleneck, and driver positions partition
// naturally by geography — which is the same insight the matcher takes further
// by making each partition single-writer and dropping the lock entirely.
type Index struct {
	mu      sync.RWMutex
	drivers map[string]Entry
	cells   map[geo.Cell]map[string]struct{}

	// Transitions counts cell changes, which is the interesting number: it is
	// the rate at which the matcher's shards will have to hand drivers over.
	transitions uint64
	// Stale counts pings dropped for arriving out of order.
	stale uint64
	// reordered counts the subset that arrived with a LOWER sequence, which
	// should not be possible and is worth separating from redelivery.
	reordered uint64
}

func NewIndex() *Index {
	return &Index{
		drivers: make(map[string]Entry),
		cells:   make(map[geo.Cell]map[string]struct{}),
	}
}

// Observation reports what changed, so the caller can emit the cell-transition
// events the matcher shards consume.
//
// Index and shard transitions are reported separately because they mean
// different things. Crossing a resolution-9 index cell is bookkeeping inside
// one shard. Crossing a resolution-7 shard cell is a handover between two
// matcher instances, and it is the only case that produces a pair of messages
// on two different Kafka partitions.
type Observation struct {
	Stale bool
	// Duplicate distinguishes a redelivery (same sequence) from a reordering
	// (lower sequence).
	Duplicate bool
	// New means this driver was not in the index at all — a cold start or a
	// driver coming online. Treated as an entry, since some shard has to learn
	// about them.
	New   bool
	Entry Entry

	IndexChanged bool
	ShardChanged bool

	PreviousIndex geo.Cell
	PreviousShard geo.Cell

	// Pickup is a pickup this ping completed: the driver was enroute_pickup
	// and is now on_trip.
	Pickup *wire.PickupObserved
}

// Observe records a ping.
//
// Out-of-order pings are dropped on the per-driver sequence number. That is not
// defensive coding: from Phase 2 a cell transition becomes two messages on two
// Kafka partitions, and Kafka orders records within a partition but not across
// them, so the two genuinely can arrive reversed. Without this a driver who has
// moved on gets resurrected in the cell they left.
func (i *Index) Observe(ping wire.DriverPing) (Observation, error) {
	point := ping.Point()

	index, err := geo.IndexCell(point)
	if err != nil {
		return Observation{}, err
	}
	shard, err := geo.ShardOf(index)
	if err != nil {
		return Observation{}, err
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	previous, existed := i.drivers[ping.DriverID]

	// Ordered by the client's own clock, within its session.
	//
	// Sequence alone was not enough once drivers moved onto WebSockets. A
	// reconnecting client briefly has two sockets, the gateway reads them with
	// two goroutines, and two pings for the same driver can reach the producer
	// in the wrong order — so a perfectly fresh position arrives behind a
	// slightly older one and, judged on sequence, is rejected. At ten thousand
	// drivers that was thirty per cent of all pings, the matcher's index went
	// stale, and four out of five ride requests found nobody.
	//
	// SentAtMs is stamped by the driver, so it increases with the sequence at
	// the source and is immune to whatever the transport does in between. The
	// sequence remains the tie-break, and remains what the matcher uses for the
	// cell handover — where the two halves genuinely travel on different
	// partitions and no clock can help.
	//
	// An older epoch is a straggler from a session that has already ended; a
	// newer one is a restarted client whose counters begin again.
	stale := existed && (ping.Epoch < previous.Epoch ||
		(ping.Epoch == previous.Epoch &&
			(ping.SentAtMs < previous.SentAtMs ||
				(ping.SentAtMs == previous.SentAtMs && ping.Seq <= previous.Seq))))

	if stale {
		i.stale++
		// Distinguishing the two matters, because they mean different things.
		// An equal sequence is a redelivery — at-least-once doing what it says,
		// and harmless. A lower one is a genuine reordering, which should be
		// impossible for records keyed by driver on a single partition, and
		// means something upstream is wrong.
		observation := Observation{
			Stale: true,
			Duplicate: ping.Epoch == previous.Epoch &&
				ping.SentAtMs == previous.SentAtMs && ping.Seq == previous.Seq,
		}
		if !observation.Duplicate {
			i.reordered++
		}
		return observation, nil
	}

	entry := Entry{
		DriverID: ping.DriverID,
		Point:    point,
		Heading:  ping.Heading,
		Status:   ping.Status,
		Epoch:    ping.Epoch,
		Seq:      ping.Seq,
		Index:    index,
		Shard:    shard,
		SentAtMs: ping.SentAtMs,
	}
	// Pickups, read from the driver's own status: setting off is the first
	// enroute_pickup ping, arriving the next on_trip one. Ingest is the one
	// place that sees every ping of every driver in order, which is what it
	// takes to see both ends of a pickup.
	var pickup *wire.PickupObserved
	switch {
	case ping.Status == wire.StatusEnRoutePickup && existed && previous.Status == wire.StatusEnRoutePickup:
		entry.EnRouteSinceMs, entry.EnRouteFrom = previous.EnRouteSinceMs, previous.EnRouteFrom
	case ping.Status == wire.StatusEnRoutePickup:
		entry.EnRouteSinceMs, entry.EnRouteFrom = ping.SentAtMs, point
	case ping.Status == wire.StatusOnTrip && existed &&
		previous.Status == wire.StatusEnRoutePickup && previous.EnRouteSinceMs > 0:
		pickup = &wire.PickupObserved{
			Tag:      wire.TagPickupObserved,
			DriverID: ping.DriverID,
			Cell:     shard.String(),
			FromLat:  previous.EnRouteFrom.Lat,
			FromLng:  previous.EnRouteFrom.Lng,
			Lat:      point.Lat,
			Lng:      point.Lng,
			Meters:   geo.DistanceMeters(previous.EnRouteFrom, point),
			Seconds:  float64(ping.SentAtMs-previous.EnRouteSinceMs) / 1000,
			AtMs:     ping.SentAtMs,
		}
	}

	i.drivers[ping.DriverID] = entry

	observation := Observation{Entry: entry, New: !existed, Pickup: pickup}

	if existed {
		observation.PreviousIndex = previous.Index
		observation.PreviousShard = previous.Shard
		observation.IndexChanged = previous.Index != index
		observation.ShardChanged = previous.Shard != shard
	}

	if observation.IndexChanged {
		i.transitions++

		if bucket, ok := i.cells[previous.Index]; ok {
			delete(bucket, ping.DriverID)
			if len(bucket) == 0 {
				// Empty buckets are removed rather than left behind: a driver
				// crossing cells all day would otherwise leak one map per cell
				// visited, and over a long run that is most of Amsterdam.
				delete(i.cells, previous.Index)
			}
		}
	}

	if observation.New || observation.IndexChanged {
		bucket, ok := i.cells[index]
		if !ok {
			bucket = make(map[string]struct{})
			i.cells[index] = bucket
		}
		bucket[ping.DriverID] = struct{}{}
	}

	return observation, nil
}

// Near returns drivers within k index-cell rings of a point, nearest first is
// NOT guaranteed — the caller ranks. This is the read the matcher will do.
func (i *Index) Near(point geo.Point, k int) ([]Entry, error) {
	origin, err := geo.IndexCell(point)
	if err != nil {
		return nil, err
	}

	ring, err := geo.Ring(origin, k)
	if err != nil {
		return nil, err
	}

	i.mu.RLock()
	defer i.mu.RUnlock()

	var found []Entry
	for _, cell := range ring {
		for id := range i.cells[cell] {
			if entry, ok := i.drivers[id]; ok {
				found = append(found, entry)
			}
		}
	}
	return found, nil
}

// Stats is what the debug endpoint and the dashboards read.
type Stats struct {
	Drivers     int    `json:"drivers"`
	Cells       int    `json:"cells"`
	Shards      int    `json:"shards"`
	Transitions uint64 `json:"cellTransitions"`
	Stale       uint64 `json:"stalePings"`
	Reordered   uint64 `json:"reorderedPings"`
}

func (i *Index) Stats() Stats {
	i.mu.RLock()
	defer i.mu.RUnlock()

	shards := make(map[geo.Cell]struct{}, len(i.cells))
	for _, entry := range i.drivers {
		shards[entry.Shard] = struct{}{}
	}

	return Stats{
		Drivers:     len(i.drivers),
		Cells:       len(i.cells),
		Shards:      len(shards),
		Transitions: i.transitions,
		Stale:       i.stale,
		Reordered:   i.reordered,
	}
}

// ShardLoad is the driver count per shard cell — the distribution the matcher
// will inherit, and the thing that decides whether sharding by geography is
// balanced or whether the city centre is one hot partition.
func (i *Index) ShardLoad() map[string]int {
	i.mu.RLock()
	defer i.mu.RUnlock()

	load := make(map[string]int)
	for _, entry := range i.drivers {
		load[entry.Shard.String()]++
	}
	return load
}
