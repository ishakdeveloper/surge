package ingest_test

import (
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/internal/ingest"
	"github.com/ishakdeveloper/surge/pkg/geo"
	"github.com/ishakdeveloper/surge/pkg/wire"
)

func ping(id string, seq uint64, point geo.Point) wire.DriverPing {
	// One session, so these tests exercise the ordering rule rather than the
	// session rule.
	return pingIn(1, id, seq, point)
}

// The property that stops a driver being resurrected in a cell they have left.
//
// From Phase 2 a cell transition is two messages on two Kafka partitions, and
// Kafka orders within a partition but not across them, so the two genuinely can
// arrive reversed. Without the sequence check the older one wins.
func TestOutOfOrderPingsAreDropped(t *testing.T) {
	index := ingest.NewIndex()
	here := geo.Point{Lat: 52.3791, Lng: 4.9003}
	there := geo.Point{Lat: 52.3600, Lng: 4.8852}

	if _, err := index.Observe(ping("drv-1", 5, there)); err != nil {
		t.Fatalf("observe: %v", err)
	}

	// An older ping for the same driver, from where they used to be.
	observation, err := index.Observe(ping("drv-1", 3, here))
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	if !observation.Stale {
		t.Fatal("a lower sequence number should be reported as stale")
	}

	near, err := index.Near(here, 1)
	if err != nil {
		t.Fatalf("near: %v", err)
	}
	if len(near) != 0 {
		t.Errorf("the stale ping resurrected the driver at their old position: %+v", near)
	}

	if stats := index.Stats(); stats.Stale != 1 || stats.Drivers != 1 {
		t.Errorf("got %+v, want 1 stale and 1 driver", stats)
	}

	// A repeat of the current sequence is equally stale — at-least-once
	// delivery means the same record can be handed over twice.
	if again, _ := index.Observe(ping("drv-1", 5, here)); !again.Stale {
		t.Error("a repeated sequence number should be stale too")
	}
}

// Moving between cells must leave nothing behind, or the index leaks a driver
// into every cell they have ever visited.
func TestCellTransitionMovesTheDriver(t *testing.T) {
	index := ingest.NewIndex()
	here := geo.Point{Lat: 52.3791, Lng: 4.9003}
	there := geo.Point{Lat: 52.3600, Lng: 4.8852}

	if _, err := index.Observe(ping("drv-1", 1, here)); err != nil {
		t.Fatalf("observe: %v", err)
	}

	observation, err := index.Observe(ping("drv-1", 2, there))
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	if !observation.IndexChanged {
		t.Fatal("moving 2 km should change the index cell")
	}
	if !observation.ShardChanged {
		t.Fatal("moving 2 km should change the shard cell too, at this distance")
	}

	atOld, _ := index.Near(here, 0)
	if len(atOld) != 0 {
		t.Errorf("driver still indexed at the old cell: %+v", atOld)
	}

	atNew, _ := index.Near(there, 0)
	if len(atNew) != 1 {
		t.Errorf("driver not indexed at the new cell, got %d entries", len(atNew))
	}

	// One driver, one occupied cell: the vacated bucket must be reclaimed
	// rather than left as an empty map.
	if stats := index.Stats(); stats.Cells != 1 {
		t.Errorf("got %d occupied cells, want 1 — empty buckets are leaking", stats.Cells)
	}
}

// The read the matcher will do: widening k must find strictly more, never less.
func TestNearWidensMonotonically(t *testing.T) {
	index := ingest.NewIndex()
	source := rand.New(rand.NewPCG(3, 4))

	for i := range 2000 {
		point := geo.Amsterdam.Random(source)
		if _, err := index.Observe(ping(fmt.Sprintf("drv-%d", i), 1, point)); err != nil {
			t.Fatalf("observe: %v", err)
		}
	}

	centre := geo.Amsterdam.Center()

	previous := -1
	for k := range 6 {
		found, err := index.Near(centre, k)
		if err != nil {
			t.Fatalf("near k=%d: %v", k, err)
		}
		if len(found) < previous {
			t.Fatalf("k=%d found %d drivers, fewer than k=%d's %d", k, len(found), k-1, previous)
		}
		previous = len(found)
	}

	if previous == 0 {
		t.Error("2000 drivers over Amsterdam and none within 5 rings of the centre")
	}
}

func TestInvalidPositionIsRejected(t *testing.T) {
	index := ingest.NewIndex()

	if _, err := index.Observe(ping("drv-1", 1, geo.Point{Lat: 999, Lng: 999})); err == nil {
		t.Error("an uncellable position should be an error, not an index entry")
	}
	if stats := index.Stats(); stats.Drivers != 0 {
		t.Errorf("rejected ping still created an entry: %+v", stats)
	}
}

// Translation is where a message stops being about a driver and starts being
// about a place. The pair of events a shard crossing produces is the part worth
// pinning: they are addressed to two different cells on purpose.
func TestTranslateShardCrossingProducesAPair(t *testing.T) {
	index := ingest.NewIndex()
	here := geo.Point{Lat: 52.3791, Lng: 4.9003}
	there := geo.Point{Lat: 52.3600, Lng: 4.8852}

	first, err := index.Observe(ping("drv-1", 1, here))
	if err != nil {
		t.Fatalf("observe: %v", err)
	}

	// A driver the index has never seen has to be announced to whichever shard
	// owns them, but there is no previous owner to tell.
	entry := ingest.Translate(first, time.Now())
	if len(entry) != 1 || entry[0].Tag != wire.TagDriverEnteredCell {
		t.Fatalf("a new driver should produce one entry event, got %d: %+v", len(entry), entry)
	}
	if entry[0].Entered.From != "" {
		t.Error("a new driver has no cell to have come from")
	}

	second, err := index.Observe(ping("drv-1", 2, there))
	if err != nil {
		t.Fatalf("observe: %v", err)
	}

	events := ingest.Translate(second, time.Now())
	if len(events) != 2 {
		t.Fatalf("a shard crossing should produce a leave and an entry, got %d", len(events))
	}

	entered, left := events[0], events[1]
	if entered.Tag != wire.TagDriverEnteredCell || left.Tag != wire.TagDriverLeftCell {
		t.Fatalf("unexpected tags: %s, %s", entered.Tag, left.Tag)
	}

	// The two are addressed to different cells, which is what puts them on
	// different Kafka partitions and is the entire reason the sequence number
	// exists.
	if entered.Cell == left.Cell {
		t.Fatal("both halves of a handover addressed to the same cell")
	}
	if entered.Entered.From != left.Cell || left.Left.To != entered.Cell {
		t.Error("the two halves do not point at each other")
	}
	if entered.Entered.Seq != left.Left.Seq {
		t.Error("the halves carry different sequence numbers, so a shard cannot order them")
	}
}

func TestTranslateWithinShardIsOneMove(t *testing.T) {
	index := ingest.NewIndex()
	start := geo.Point{Lat: 52.3791, Lng: 4.9003}

	first, _ := index.Observe(ping("drv-1", 1, start))
	_ = ingest.Translate(first, time.Now())

	// ~30 m: a new resolution-9 index cell is possible, the resolution-7 shard
	// is not going to change.
	nudged := geo.Point{Lat: start.Lat + 0.0003, Lng: start.Lng}
	second, err := index.Observe(ping("drv-1", 2, nudged))
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	if second.ShardChanged {
		t.Skip("that nudge happened to cross a shard boundary; not what this test is about")
	}

	events := ingest.Translate(second, time.Now())
	if len(events) != 1 || events[0].Tag != wire.TagDriverMoved {
		t.Fatalf("a move inside one shard should be a single DriverMoved, got %+v", events)
	}
}

func TestTranslateDropsStale(t *testing.T) {
	if events := ingest.Translate(ingest.Observation{Stale: true}, time.Now()); events != nil {
		t.Errorf("a stale observation should translate to nothing, got %+v", events)
	}
}

// A sequence number is only meaningful inside its session.
//
// This is a real bug the load rig caught, not a hypothetical. Restarting the
// simulator with ingest still running froze the entire fleet: every driver's
// counter went back to 1, every subsequent ping was rejected as a reordering,
// and 2,000 drivers sat at their last known position permanently because the
// counter could never catch up. The real-world version is a driver reinstalling
// the app.
func TestClientRestartIsNotAReordering(t *testing.T) {
	index := ingest.NewIndex()
	here := geo.Point{Lat: 52.3791, Lng: 4.9003}
	there := geo.Point{Lat: 52.3600, Lng: 4.8852}

	const firstSession, secondSession = 100, 200

	// A long-running session gets well ahead.
	for seq := uint64(1); seq <= 400; seq++ {
		if _, err := index.Observe(pingIn(firstSession, "drv-1", seq, here)); err != nil {
			t.Fatalf("observe: %v", err)
		}
	}

	// The client restarts: new session, counter back to one, and the driver has
	// moved in the meantime.
	observation, err := index.Observe(pingIn(secondSession, "drv-1", 1, there))
	if err != nil {
		t.Fatalf("observe: %v", err)
	}

	if observation.Stale {
		t.Fatal("a restarted client was rejected as stale; that driver is now frozen forever")
	}
	if !observation.ShardChanged {
		t.Error("the new session's position was not applied")
	}

	found, _ := index.Near(there, 0)
	if len(found) != 1 {
		t.Errorf("the driver is not at their new position, got %d entries", len(found))
	}
}

// The other direction: packets from a session that has already ended must not
// resurrect an old position.
func TestStragglersFromAnEndedSessionAreDropped(t *testing.T) {
	index := ingest.NewIndex()
	here := geo.Point{Lat: 52.3791, Lng: 4.9003}
	there := geo.Point{Lat: 52.3600, Lng: 4.8852}

	if _, err := index.Observe(pingIn(100, "drv-1", 5, here)); err != nil {
		t.Fatalf("observe: %v", err)
	}
	if _, err := index.Observe(pingIn(200, "drv-1", 1, there)); err != nil {
		t.Fatalf("observe: %v", err)
	}

	// A late packet from the first session, with a much higher sequence.
	observation, err := index.Observe(pingIn(100, "drv-1", 999, here))
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	if !observation.Stale {
		t.Fatal("a straggler from an ended session overwrote the current one")
	}
	if observation.Duplicate {
		t.Error("an old-epoch straggler is not a duplicate")
	}
}

// Within one session the ordering rule is unchanged, and redelivery is still
// recognised as redelivery rather than as a reordering.
func TestDuplicateWithinASessionIsReportedAsDuplicate(t *testing.T) {
	index := ingest.NewIndex()
	here := geo.Point{Lat: 52.3791, Lng: 4.9003}

	if _, err := index.Observe(pingIn(100, "drv-1", 5, here)); err != nil {
		t.Fatalf("observe: %v", err)
	}

	observation, err := index.Observe(pingIn(100, "drv-1", 5, here))
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	if !observation.Stale || !observation.Duplicate {
		t.Errorf("a redelivered ping should be stale and a duplicate, got %+v", observation)
	}
}

func pingIn(epoch uint64, id string, seq uint64, point geo.Point) wire.DriverPing {
	return wire.DriverPing{
		DriverID: id, Epoch: epoch, Seq: seq,
		Lat: point.Lat, Lng: point.Lng, Status: wire.StatusIdle,
	}
}
