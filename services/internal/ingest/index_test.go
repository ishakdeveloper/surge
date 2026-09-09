package ingest_test

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/ishakdeveloper/surge/internal/ingest"
	"github.com/ishakdeveloper/surge/pkg/geo"
	"github.com/ishakdeveloper/surge/pkg/wire"
)

func ping(id string, seq uint64, point geo.Point) wire.DriverPing {
	return wire.DriverPing{
		DriverID: id, Seq: seq, Lat: point.Lat, Lng: point.Lng, Status: wire.StatusIdle,
	}
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
	if !observation.Transition {
		t.Fatal("moving 2 km should be a cell transition")
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
