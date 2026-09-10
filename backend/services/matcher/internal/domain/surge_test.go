package domain_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/matcher/internal/domain"
	"github.com/ishakdeveloper/surge/shared/wire"
)

// Batched, so an idle car stays idle: requests wait for a window that these
// tests never close, instead of reserving the car on arrival.
func surgeShard() *domain.Shard {
	config := domain.DefaultConfig()
	config.Strategy = domain.StrategyBatched
	return domain.NewShard(0, config)
}

var trips int

// ask brings n new riders to the pickup, now.
func ask(t *testing.T, shard *domain.Shard, n int, now time.Time) {
	t.Helper()
	for range n {
		trips++
		must(t, shard, request(t, fmt.Sprintf("surge-%d", trips), dam), now)
	}
}

// sustain keeps rate riders a minute asking for minutes, looking every minute,
// and returns the last look.
func sustain(t *testing.T, shard *domain.Shard, rate int, from time.Time, minutes int) (surging, changed []wire.CellSurge, end time.Time) {
	t.Helper()
	end = from
	for range minutes {
		end = end.Add(time.Minute)
		ask(t, shard, rate, end)
		surging, changed = shard.Surge(end)
	}
	return surging, changed, end
}

func at(surges []wire.CellSurge, cell string) (wire.CellSurge, bool) {
	for _, surge := range surges {
		if surge.Cell == cell {
			return surge, true
		}
	}
	return wire.CellSurge{}, false
}

// Three riders a minute for one idle car is 1.5x, once the averages settle.
func TestSurgeIsRidersPerMinuteAgainstIdleCars(t *testing.T) {
	shard := surgeShard()
	must(t, shard, entered(t, "d1", dam, 1), base)
	shard.Surge(base)

	surging, _, _ := sustain(t, shard, 3, base, 20)
	if surge, ok := at(surging, shardCell(t, dam)); !ok || surge.Multiplier != 1.5 {
		t.Fatalf("surge = %+v, want 1.5x for 3 riders a minute and 1 car", surge)
	}
}

// A rider turned away for want of a car still counts. Greedy abandons a
// request with no candidate on arrival, and that rider is exactly who surge
// is for.
func TestRidersWithNoCarStillCountAsDemand(t *testing.T) {
	shard := domain.NewShard(0, domain.DefaultConfig())
	shard.Surge(base)

	surging, _, _ := sustain(t, shard, 2, base, 20)
	surge, ok := at(surging, shardCell(t, dam))
	if !ok || surge.Multiplier != 2 {
		t.Fatalf("surge = %+v, want 2x for 2 riders a minute and no cars", surge)
	}
}

// One rider against one idle car is a balanced market, not a surge.
func TestOneRiderForOneCarIsBasePrice(t *testing.T) {
	shard := surgeShard()
	must(t, shard, entered(t, "d1", dam, 1), base)
	shard.Surge(base)

	surging, _, _ := sustain(t, shard, 1, base, 20)
	if len(surging) != 0 {
		t.Errorf("one rider a minute for one car surged: %+v", surging)
	}
}

// Demand counts riders asking over the last minute or so, so a burst moves the
// price at once — ten riders now are ten in the last minute — but the price
// comes down over minutes rather than the moment the burst ends, and it never
// passes the cap.
func TestSurgeHoldsTheCapAndDecaysOverMinutes(t *testing.T) {
	shard := surgeShard()
	cell := shardCell(t, dam)
	shard.Surge(base)

	surging, _, busy := sustain(t, shard, 10, base, 20)
	if capped, _ := at(surging, cell); capped.Multiplier != 3 {
		t.Fatalf("ten riders a minute and no cars = %.1fx, want the 3x cap", capped.Multiplier)
	}
	surging, _ = shard.Surge(busy.Add(10 * time.Second))
	if still, ok := at(surging, cell); !ok || still.Multiplier <= 1 {
		t.Errorf("ten seconds after the riders stopped the price was already back: %+v", surging)
	}
}

// Plenty of idle cars means base price, and nothing is written for a cell at 1.0.
func TestIdleCarsOutnumberingRidersIsBasePrice(t *testing.T) {
	shard := surgeShard()
	for i := range 5 {
		must(t, shard, entered(t, fmt.Sprintf("car-%d", i), dam, 1), base)
	}
	shard.Surge(base)

	surging, changed, _ := sustain(t, shard, 2, base, 20)
	if len(surging) != 0 || len(changed) != 0 {
		t.Errorf("surging %+v, changed %+v; want neither", surging, changed)
	}
}

// An unchanged price is not rewritten every look, but is repeated on the
// heartbeat so the trip service can tell steady from stopped.
func TestAnUnchangedPriceIsRepublishedOnlyOnTheHeartbeat(t *testing.T) {
	shard := surgeShard()
	shard.Surge(base)
	_, _, settled := sustain(t, shard, 10, base, 30)

	ask(t, shard, 10, settled.Add(time.Second))
	if _, changed := shard.Surge(settled.Add(time.Second)); len(changed) != 0 {
		t.Errorf("a steady price was republished a second later: %+v", changed)
	}
	if _, changed, _ := sustain(t, shard, 10, settled.Add(time.Second), 1); len(changed) != 1 {
		t.Errorf("the heartbeat did not republish the steady price: %+v", changed)
	}
}

// When the riders stop asking the price decays back to 1.0, says so once, and
// the cell is forgotten.
func TestSurgeFallsBackToBasePriceAndSaysSo(t *testing.T) {
	shard := surgeShard()
	cell := shardCell(t, dam)
	shard.Surge(base)
	_, _, busy := sustain(t, shard, 10, base, 20)

	surging, changed := shard.Surge(busy.Add(2 * time.Hour))
	if len(surging) != 0 {
		t.Errorf("still surging with nobody asking: %+v", surging)
	}
	if back, ok := at(changed, cell); !ok || back.Multiplier != 1 {
		t.Errorf("the return to 1.0 was not published: %+v", changed)
	}
	if _, again := shard.Surge(busy.Add(3 * time.Hour)); len(again) != 0 {
		t.Errorf("a quiet cell kept publishing: %+v", again)
	}
}
