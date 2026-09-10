package domain

import (
	"math"
	"sort"
	"time"

	"github.com/ishakdeveloper/surge/shared/wire"
)

// Surge pricing, per resolution-7 cell.
//
// The shard that owns a cell is the only process that sees both halves of its
// market: the riders asking for a car there, and the cars standing idle there.
// So the multiplier is computed here, from those two, each smoothed over a
// minute so that a burst of requests moves the price gradually rather than
// flipping it:
//
//	ratio      = (riders asking per minute + 1) / (idle cars + 1)
//	multiplier = 1 + SurgeStep × (ratio − 1), between 1.0 and SurgeMax
//
// Demand is every rider who asks, found a car or not: the rider turned away is
// the one surge is for, and a count of requests still being matched would
// never see them. The +1s keep a quiet cell at exactly 1.0 and stop one rider
// in a carless cell from reading as infinite demand. Three riders a minute for
// one idle car is 1.5×.
// Rounded down to a tenth, because a price that changes every second is one
// nobody believes.

type surgeCell struct {
	// demand is riders asking per minute; supply is idle cars.
	demand, supply float64
	// published is the multiplier last sent to surge.cells, and when.
	published   float64
	publishedAt time.Time
}

// Surge brings each cell's smoothed demand and supply up to now, and returns
// every cell priced above 1.0 — what the console draws — and the cells whose
// price changed or is due a heartbeat — what surge.cells needs to hear. A cell
// falling back to 1.0 is reported once, so the trip service stops charging it.
func (s *Shard) Surge(now time.Time) (surging, changed []wire.CellSurge) {
	surging, changed = []wire.CellSurge{}, []wire.CellSurge{}
	if s.surgeAt.IsZero() || !now.After(s.surgeAt) {
		// The first look starts the clock; averages begin at zero and rise.
		s.surgeAt = now
		clear(s.arrivals)
		return surging, changed
	}
	elapsed := now.Sub(s.surgeAt)
	alpha := 1 - math.Exp(-elapsed.Seconds()/s.config.SurgeWindow.Seconds())
	s.surgeAt = now

	demand := map[string]float64{}
	for cell, count := range s.arrivals {
		demand[cell] = float64(count) / elapsed.Minutes()
	}
	clear(s.arrivals)
	supply := map[string]float64{}
	for _, driver := range s.drivers {
		if driver.available() {
			supply[driver.shardCell]++
		}
	}

	for cell := range demand {
		s.track(cell)
	}
	for cell := range supply {
		s.track(cell)
	}

	for cell, c := range s.surge {
		c.demand += alpha * (demand[cell] - c.demand)
		c.supply += alpha * (supply[cell] - c.supply)
		multiplier := s.multiplier(c.demand, c.supply)

		surge := wire.CellSurge{
			Tag: wire.TagCellSurge, Cell: cell, Multiplier: multiplier,
			Demand: round2(c.demand), Supply: round2(c.supply), AtMs: now.UnixMilli(),
		}
		if multiplier > 1 {
			surging = append(surging, surge)
		}
		heartbeat := multiplier > 1 && now.Sub(c.publishedAt) >= s.config.SurgeHeartbeat
		if multiplier != c.published || heartbeat {
			changed = append(changed, surge)
			c.published, c.publishedAt = multiplier, now
		}
		// A quiet cell at base price is forgotten; it is re-tracked the moment
		// a rider or a car turns up in it.
		if multiplier == 1 && c.demand < 0.01 && c.supply < 0.01 {
			delete(s.surge, cell)
		}
	}

	sort.Slice(surging, func(a, b int) bool { return surging[a].Cell < surging[b].Cell })
	sort.Slice(changed, func(a, b int) bool { return changed[a].Cell < changed[b].Cell })
	return surging, changed
}

func (s *Shard) track(cell string) {
	if _, ok := s.surge[cell]; !ok {
		// Published as 1.0 from the start: a cell at base price never needs
		// telling the trip service anything.
		s.surge[cell] = &surgeCell{published: 1}
	}
}

func (s *Shard) multiplier(demand, supply float64) float64 {
	ratio := (demand + 1) / (supply + 1)
	multiplier := math.Max(1, math.Min(s.config.SurgeMax, 1+s.config.SurgeStep*(ratio-1)))
	// Down to the tenth, with a thousandth of slack: a smoothed average only ever
	// approaches its target, and 3 riders for 1 car converging on 1.49998x is
	// 1.5x, not 1.4x.
	return math.Floor(multiplier*10+0.01) / 10
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
