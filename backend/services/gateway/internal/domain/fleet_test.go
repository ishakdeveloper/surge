package domain_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/gateway/internal/domain"
	"github.com/ishakdeveloper/surge/shared/geo"
	"github.com/ishakdeveloper/surge/shared/wire"
)

var epoch = time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

func driver(t *testing.T, id string, lat, lng float64, status wire.DriverStatus, reserved bool) wire.FleetDriver {
	t.Helper()
	cell, err := geo.ShardCell(geo.Point{Lat: lat, Lng: lng})
	if err != nil {
		t.Fatalf("cell: %v", err)
	}
	return wire.FleetDriver{ID: id, Lat: lat, Lng: lng, Status: status, Cell: cell.String(), Reserved: reserved}
}

var city = wire.Viewport{West: 4.7, South: 52.2, East: 5.1, North: 52.5, Zoom: 11}

// Zoomed out, the console gets counts per cell — idle excludes reserved
// drivers, whose own status still says idle — and each cell's hexagon.
func TestZoomedOutIsCellCounts(t *testing.T) {
	fleet := domain.NewFleet()
	fleet.Record(wire.FleetFrame{Partition: 3, Instance: "matcher-a", Drivers: []wire.FleetDriver{
		driver(t, "a", 52.3791, 4.9003, wire.StatusIdle, false),
		driver(t, "b", 52.3792, 4.9004, wire.StatusIdle, true),
		driver(t, "c", 52.3793, 4.9005, wire.StatusOnTrip, false),
	}, Pending: 1, Offers: 1}, epoch)

	update := fleet.Snapshot(city, epoch)

	if update.Mode != "cells" || len(update.Drivers) != 0 {
		t.Fatalf("zoomed out should be cells only, got mode %q with %d drivers", update.Mode, len(update.Drivers))
	}
	if len(update.Cells) != 1 || update.Cells[0].Drivers != 3 || update.Cells[0].Idle != 1 {
		t.Fatalf("cells = %+v", update.Cells)
	}
	if len(update.Cells[0].Boundary) != 6 {
		t.Errorf("a hexagon has six vertices, got %d", len(update.Cells[0].Boundary))
	}
	if update.Stats.Drivers != 3 || update.Stats.Idle != 1 || update.Stats.Pending != 1 {
		t.Errorf("stats = %+v", update.Stats)
	}
}

// Zoomed in, only the drivers inside the viewport, and no cells.
func TestZoomedInIsDriversInView(t *testing.T) {
	fleet := domain.NewFleet()
	fleet.Record(wire.FleetFrame{Partition: 1, Drivers: []wire.FleetDriver{
		driver(t, "near", 52.3791, 4.9003, wire.StatusIdle, false),
		driver(t, "far", 52.3000, 4.8000, wire.StatusIdle, false),
	}}, epoch)

	view := wire.Viewport{West: 4.89, South: 52.37, East: 4.91, North: 52.39, Zoom: 15}
	update := fleet.Snapshot(view, epoch)

	if update.Mode != "drivers" || len(update.Cells) != 0 {
		t.Fatalf("zoomed in should be drivers only, got mode %q with %d cells", update.Mode, len(update.Cells))
	}
	if len(update.Drivers) != 1 || update.Drivers[0].ID != "near" {
		t.Fatalf("drivers = %+v", update.Drivers)
	}
	// The totals still describe the whole fleet, not the viewport.
	if update.Stats.Drivers != 2 {
		t.Errorf("stats count %d drivers, want the whole fleet's 2", update.Stats.Drivers)
	}
}

// A partition nobody has reported for three seconds is not drawn — its owner
// died or handed it over, and drawing it would show its drivers where they
// were, or twice.
func TestStaleFramesAreNotDrawn(t *testing.T) {
	fleet := domain.NewFleet()
	fleet.Record(wire.FleetFrame{Partition: 1, Drivers: []wire.FleetDriver{driver(t, "old", 52.3791, 4.9003, wire.StatusIdle, false)}}, epoch)
	fleet.Record(wire.FleetFrame{Partition: 2, Drivers: []wire.FleetDriver{driver(t, "new", 52.3791, 4.9003, wire.StatusIdle, false)}}, epoch.Add(3*time.Second))

	update := fleet.Snapshot(city, epoch.Add(4*time.Second))

	if len(update.Shards) != 1 || update.Shards[0].Partition != 2 || update.Stats.Drivers != 1 {
		t.Fatalf("only partition 2 is live, got shards %+v and %d drivers", update.Shards, update.Stats.Drivers)
	}
	if _, found := fleet.Driver("old", epoch.Add(4*time.Second)); found {
		t.Error("a rider was told the position of a driver from a stale frame")
	}
}

// Percentiles and rates come from the frames' own latencies, over a window.
func TestLatencyPercentilesOverTheWindow(t *testing.T) {
	fleet := domain.NewFleet()
	var latencies []int64
	for ms := int64(100); ms <= 10_000; ms += 100 {
		latencies = append(latencies, ms)
	}
	fleet.Record(wire.FleetFrame{Partition: 1, MatchLatenciesMs: latencies, Abandoned: 5}, epoch)

	stats := fleet.Snapshot(city, epoch).Stats
	if stats.P50Ms != 5000 || stats.P95Ms != 9500 || stats.P99Ms != 9900 {
		t.Errorf("percentiles = %d / %d / %d, want 5000 / 9500 / 9900", stats.P50Ms, stats.P95Ms, stats.P99Ms)
	}
	if stats.MatchedPerSecond != 10 || stats.AbandonedPerSecond != 0.5 {
		t.Errorf("rates = %.2f matched, %.2f abandoned per second", stats.MatchedPerSecond, stats.AbandonedPerSecond)
	}

	// Past the window they no longer count.
	later := fleet.Snapshot(city, epoch.Add(11*time.Second)).Stats
	if later.P99Ms != 0 || later.MatchedPerSecond != 0 {
		t.Errorf("samples outlived the window: %+v", later)
	}
}

// encoding/json writes a nil slice as null, and the browser's schema for an
// array rejects null. An empty fleet must still be arrays.
func TestAnEmptyFleetEncodesArraysNotNull(t *testing.T) {
	payload, err := json.Marshal(domain.NewFleet().Snapshot(city, epoch))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(payload), "null") {
		t.Errorf("an empty update contains null: %s", payload)
	}
}
