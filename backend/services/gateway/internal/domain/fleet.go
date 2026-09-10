package domain

import (
	"sort"
	"sync"
	"time"

	"github.com/ishakdeveloper/surge/shared/geo"
	"github.com/ishakdeveloper/surge/shared/wire"
)

const (
	// FrameTTL is how long a partition's latest frame stays on the map.
	//
	// A partition not heard from in three seconds has an owner that died, or
	// has just been handed to an instance whose first frame is not here yet.
	// Keeping the old frame would draw its drivers where they were; keeping
	// both old and new would draw every one of them twice.
	FrameTTL = 3 * time.Second

	// LatencyWindow is what the percentiles and rates are computed over.
	LatencyWindow = 10 * time.Second

	// DriverZoom is where a console stops seeing cells and starts seeing
	// drivers. Below it the city is a few hundred cell counts; above it the
	// viewport is small enough that its drivers are a few hundred points.
	DriverZoom = 14.0

	// MaxDrivers caps a drivers-mode update, so a console zoomed in on the
	// busiest street still receives a frame it can decode in time.
	MaxDrivers = 3000
)

// Fleet is the gateway's picture of the city: the latest frame from every
// matcher partition, and the recent match latencies they reported.
//
// Written by one consumer goroutine and read by the fan-out, so a mutex, and a
// short one — nothing is held across I/O.
type Fleet struct {
	mu         sync.Mutex
	frames     map[int32]heard
	latencies  []sample
	abandons   []sample
	boundaries map[string][][2]float64
}

type heard struct {
	frame wire.FleetFrame
	at    time.Time
}

type sample struct {
	at    time.Time
	value int64
}

func NewFleet() *Fleet {
	return &Fleet{frames: map[int32]heard{}, boundaries: map[string][][2]float64{}}
}

// Record keeps a partition's latest frame.
func (f *Fleet) Record(frame wire.FleetFrame, now time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.frames[frame.Partition] = heard{frame: frame, at: now}
	for _, ms := range frame.MatchLatenciesMs {
		f.latencies = append(f.latencies, sample{at: now, value: ms})
	}
	if frame.Abandoned > 0 {
		f.abandons = append(f.abandons, sample{at: now, value: int64(frame.Abandoned)})
	}
	f.prune(now)
}

// prune drops samples older than the window. They arrive in time order, so the
// stale ones are always a prefix.
func (f *Fleet) prune(now time.Time) {
	cutoff := now.Add(-LatencyWindow)
	f.latencies = dropBefore(f.latencies, cutoff)
	f.abandons = dropBefore(f.abandons, cutoff)
}

func dropBefore(samples []sample, cutoff time.Time) []sample {
	keep := sort.Search(len(samples), func(i int) bool { return !samples[i].at.Before(cutoff) })
	return samples[keep:]
}

// Snapshot is one console's view, shaped by its viewport.
func (f *Fleet) Snapshot(view wire.Viewport, now time.Time) wire.FleetUpdate {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prune(now)

	update := wire.FleetUpdate{
		AtMs:    now.UnixMilli(),
		Mode:    "cells",
		Cells:   []wire.FleetCell{},
		Drivers: []wire.FleetDriver{},
		Shards:  []wire.FleetShard{},
	}
	zoomedIn := view.Zoom >= DriverZoom
	if zoomedIn {
		update.Mode = "drivers"
	}

	cells := map[string]*wire.FleetCell{}

	for partition, latest := range f.frames {
		age := now.Sub(latest.at)
		if age > FrameTTL {
			continue
		}

		update.Shards = append(update.Shards, wire.FleetShard{
			Partition: partition,
			Instance:  latest.frame.Instance,
			Drivers:   len(latest.frame.Drivers),
			Pending:   latest.frame.Pending,
			Offers:    latest.frame.Offers,
			AgeMs:     age.Milliseconds(),
		})
		update.Stats.Pending += latest.frame.Pending
		update.Stats.Offers += latest.frame.Offers

		for _, driver := range latest.frame.Drivers {
			idle := driver.Status == wire.StatusIdle && !driver.Reserved
			update.Stats.Drivers++
			if idle {
				update.Stats.Idle++
			}

			if zoomedIn {
				if len(update.Drivers) < MaxDrivers && inside(view, driver) {
					update.Drivers = append(update.Drivers, driver)
				}
				continue
			}

			cell, ok := cells[driver.Cell]
			if !ok {
				cell = &wire.FleetCell{Cell: driver.Cell, Boundary: f.boundary(driver.Cell)}
				cells[driver.Cell] = cell
			}
			cell.Drivers++
			if idle {
				cell.Idle++
			}
		}
	}

	for _, cell := range cells {
		update.Cells = append(update.Cells, *cell)
	}
	sort.Slice(update.Cells, func(a, b int) bool { return update.Cells[a].Cell < update.Cells[b].Cell })
	sort.Slice(update.Shards, func(a, b int) bool { return update.Shards[a].Partition < update.Shards[b].Partition })

	window := LatencyWindow.Seconds()
	update.Stats.MatchedPerSecond = float64(len(f.latencies)) / window
	var abandoned int64
	for _, s := range f.abandons {
		abandoned += s.value
	}
	update.Stats.AbandonedPerSecond = float64(abandoned) / window

	ordered := make([]int64, len(f.latencies))
	for i, s := range f.latencies {
		ordered[i] = s.value
	}
	sort.Slice(ordered, func(a, b int) bool { return ordered[a] < ordered[b] })
	update.Stats.P50Ms = percentile(ordered, 0.50)
	update.Stats.P95Ms = percentile(ordered, 0.95)
	update.Stats.P99Ms = percentile(ordered, 0.99)

	return update
}

// Driver finds one driver in the live frames, for a rider following their
// trip. A scan, and that is fine: it runs once a second per follower over a
// fleet of thousands, not per ping.
func (f *Fleet) Driver(id string, now time.Time) (wire.FleetDriver, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, latest := range f.frames {
		if now.Sub(latest.at) > FrameTTL {
			continue
		}
		for _, driver := range latest.frame.Drivers {
			if driver.ID == id {
				return driver, true
			}
		}
	}
	return wire.FleetDriver{}, false
}

// boundary is a cell's hexagon, computed once and kept: a cell's shape never
// changes, and there are only a few hundred of them in the city.
func (f *Fleet) boundary(cell string) [][2]float64 {
	if cached, ok := f.boundaries[cell]; ok {
		return cached
	}
	outline := [][2]float64{}
	if points, err := geo.Boundary(cell); err == nil {
		for _, point := range points {
			outline = append(outline, [2]float64{point.Lng, point.Lat})
		}
	}
	f.boundaries[cell] = outline
	return outline
}

func inside(view wire.Viewport, driver wire.FleetDriver) bool {
	return driver.Lat >= view.South && driver.Lat <= view.North &&
		driver.Lng >= view.West && driver.Lng <= view.East
}

// percentile by nearest rank, over an ascending slice. Zero when there is
// nothing to rank, which the console shows as "-".
func percentile(ascending []int64, p float64) int64 {
	if len(ascending) == 0 {
		return 0
	}
	rank := int(float64(len(ascending))*p+0.999999) - 1
	rank = max(0, min(rank, len(ascending)-1))
	return ascending[rank]
}
