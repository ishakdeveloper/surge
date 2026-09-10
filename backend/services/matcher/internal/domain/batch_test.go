package domain_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/matcher/internal/domain"
	"github.com/ishakdeveloper/surge/shared/geo"
	"github.com/ishakdeveloper/surge/shared/wire"
)

// The greedy trap, on a street. Rider A stands between two cars; rider B is
// four hundred metres north, near only the northern one.
//
//	B ·························· 445 m north of A
//	d1 ························· 111 m north of A
//	A
//	d2 ························· 167 m south of A
//
// Greedy serves A first and gives it the nearest car, d1, which sends d2 six
// hundred metres to B: 722 m in total. Solved together, A takes d2 and B takes
// d1: 500 m.
var (
	riderA = geo.Point{Lat: 52.3730, Lng: 4.8926}
	riderB = geo.Point{Lat: 52.3770, Lng: 4.8926}
	north  = geo.Point{Lat: 52.3740, Lng: 4.8926}
	south  = geo.Point{Lat: 52.3715, Lng: 4.8926}
)

func batched() *domain.Shard {
	config := domain.DefaultConfig()
	config.Strategy = domain.StrategyBatched
	return domain.NewShard(0, config)
}

func withTwoCarsAndTwoRiders(t *testing.T, shard *domain.Shard) {
	t.Helper()
	must(t, shard, entered(t, "d1", north, 1), base)
	must(t, shard, entered(t, "d2", south, 1), base)
	for _, event := range []wire.GeoEvent{request(t, "A", riderA), request(t, "B", riderB)} {
		if out := must(t, shard, event, base); len(out.Offers) != 0 {
			t.Fatalf("a batched shard offered on arrival: %+v", out.Offers)
		}
	}
}

func pickupTotal(outcomes ...domain.Outcome) float64 {
	total := 0.0
	for _, outcome := range outcomes {
		for _, dispatch := range outcome.Dispatches {
			total += dispatch.Meters
		}
	}
	return total
}

func near(t *testing.T, what string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1 {
		t.Errorf("%s = %.0f m, want %.0f m", what, got, want)
	}
}

func offered(outcome domain.Outcome) map[string]string {
	byTrip := map[string]string{}
	for _, offer := range outcome.Offers {
		byTrip[offer.TripID] = offer.DriverID
	}
	return byTrip
}

// What greedy does with the same street, so the batched test below is
// measured against something rather than against an expectation.
func TestGreedyFallsIntoTheTrap(t *testing.T) {
	shard := domain.NewShard(0, domain.DefaultConfig())
	must(t, shard, entered(t, "d1", north, 1), base)
	must(t, shard, entered(t, "d2", south, 1), base)

	first := must(t, shard, request(t, "A", riderA), base)
	second := must(t, shard, request(t, "B", riderB), base)
	got := offered(first)
	for trip, driver := range offered(second) {
		got[trip] = driver
	}
	if got["A"] != "d1" || got["B"] != "d2" {
		t.Errorf("greedy = %v, want A→d1 and B→d2", got)
	}
	near(t, "greedy pickup total", pickupTotal(first, second),
		geo.DistanceMeters(riderA, north)+geo.DistanceMeters(riderB, south))
}

func TestBatchingSolvesTheWindowTogether(t *testing.T) {
	shard := batched()
	withTwoCarsAndTwoRiders(t, shard)

	if early := shard.Tick(base.Add(time.Second)); early.Batch != nil {
		t.Fatal("the batch closed before its window")
	}
	tick := shard.Tick(base.Add(2 * time.Second))
	if tick.Batch == nil || len(tick.Batch.Pickups) != 2 || len(tick.Batch.Drivers) != 2 {
		t.Fatalf("batch = %+v, want two pickups over two drivers", tick.Batch)
	}

	// No travel times: the solve falls back to distance, which is enough.
	solved := shard.Solved(domain.BatchResult{ID: tick.Batch.ID, Err: errors.New("valhalla down")}, base.Add(2*time.Second))
	if got := offered(solved); got["A"] != "d2" || got["B"] != "d1" {
		t.Errorf("batched = %v, want A→d2 and B→d1", got)
	}
	// 500 m against greedy's 722 m on the same street.
	near(t, "batched pickup total", pickupTotal(solved),
		geo.DistanceMeters(riderA, south)+geo.DistanceMeters(riderB, north))
}

// Travel times beat distance. Here the matrix says d1 is a minute from A and
// d2 is across a canal from it — the opposite of what the map suggests — and
// the assignment follows the roads.
func TestTravelTimesOverruleDistance(t *testing.T) {
	shard := batched()
	withTwoCarsAndTwoRiders(t, shard)
	tick := shard.Tick(base.Add(2 * time.Second))

	seconds := make([][]float64, len(tick.Batch.Drivers))
	for j, point := range tick.Batch.Drivers {
		seconds[j] = make([]float64, len(tick.Batch.Pickups))
		for i := range tick.Batch.Pickups {
			// Pickups are in arrival order: A, then B.
			switch {
			case point == north && i == 0:
				seconds[j][i] = 60
			case point == north:
				seconds[j][i] = 900
			case i == 0:
				seconds[j][i] = 800
			default:
				seconds[j][i] = 70
			}
		}
	}

	solved := shard.Solved(domain.BatchResult{ID: tick.Batch.ID, Seconds: seconds}, base.Add(3*time.Second))
	if got := offered(solved); got["A"] != "d1" || got["B"] != "d2" {
		t.Errorf("batched over travel times = %v, want A→d1 and B→d2", got)
	}
}

// An answer to a batch that is no longer in flight changes nothing.
func TestAStaleResultIsIgnored(t *testing.T) {
	shard := batched()
	withTwoCarsAndTwoRiders(t, shard)
	tick := shard.Tick(base.Add(2 * time.Second))

	if stale := shard.Solved(domain.BatchResult{ID: tick.Batch.ID + 1}, base.Add(3*time.Second)); len(stale.Offers) != 0 {
		t.Fatalf("a result for another batch dispatched: %+v", stale.Offers)
	}
	if got := offered(shard.Solved(domain.BatchResult{ID: tick.Batch.ID, Err: errors.New("x")}, base.Add(3*time.Second))); len(got) != 2 {
		t.Errorf("the real result dispatched %v", got)
	}
}

// A router that never answers does not hold riders hostage: the batch is
// solved over distance when the timeout passes, and the late answer is ignored.
func TestARouterThatNeverAnswersTimesOut(t *testing.T) {
	shard := batched()
	withTwoCarsAndTwoRiders(t, shard)
	tick := shard.Tick(base.Add(2 * time.Second))

	if waiting := shard.Tick(base.Add(4 * time.Second)); len(waiting.Offers) != 0 {
		t.Fatalf("dispatched before the timeout: %+v", waiting.Offers)
	}
	timedOut := shard.Tick(base.Add(2*time.Second + domain.DefaultConfig().BatchTimeout))
	if got := offered(timedOut); got["A"] != "d2" || got["B"] != "d1" {
		t.Errorf("after the timeout = %v, want the distance solve A→d2 and B→d1", got)
	}
	if late := shard.Solved(domain.BatchResult{ID: tick.Batch.ID}, base.Add(10*time.Second)); len(late.Offers) != 0 {
		t.Errorf("the late answer dispatched again: %+v", late.Offers)
	}
}

// A request arriving while a batch is being solved waits for the next window
// rather than joining one whose matrix has already been asked for.
func TestARequestDuringASolveWaitsForTheNextWindow(t *testing.T) {
	shard := batched()
	withTwoCarsAndTwoRiders(t, shard)
	must(t, shard, entered(t, "d3", nearby, 1), base)
	tick := shard.Tick(base.Add(2 * time.Second))

	late := base.Add(2500 * time.Millisecond)
	if out := must(t, shard, request(t, "C", dam), late); len(out.Offers) != 0 {
		t.Fatalf("C was offered on arrival: %+v", out.Offers)
	}
	first := offered(shard.Solved(domain.BatchResult{ID: tick.Batch.ID, Err: errors.New("x")}, late))
	if _, included := first["C"]; included || len(first) != 2 {
		t.Fatalf("first batch dispatched %v, want A and B only", first)
	}

	next := shard.Tick(late.Add(2 * time.Second))
	if next.Batch == nil || len(next.Batch.Pickups) != 1 {
		t.Fatalf("next batch = %+v, want C alone", next.Batch)
	}
}

// Between asking for travel times and getting them, another shard reserves
// the car the solve is about to choose. Nobody is double-booked: the request
// that wanted it moves on to the next candidate, exactly as greedy would.
func TestACarTakenDuringTheSolveIsNotDoubleBooked(t *testing.T) {
	shard := batched()
	withTwoCarsAndTwoRiders(t, shard)
	tick := shard.Tick(base.Add(2 * time.Second))

	must(t, shard, wire.GeoEvent{
		Tag:  wire.TagReserveDriver,
		Cell: shardCell(t, south),
		AtMs: base.UnixMilli(),
		Reserve: &wire.ReservePayload{
			DriverID: "d2", TripID: "elsewhere", ReplyCell: shardCell(t, dam),
			DeadlineMs: base.Add(time.Minute).UnixMilli(), RiderID: "rider-elsewhere",
			PickupLat: south.Lat, PickupLng: south.Lng,
		},
	}, base.Add(2*time.Second))

	solved := shard.Solved(domain.BatchResult{ID: tick.Batch.ID, Err: errors.New("x")}, base.Add(3*time.Second))
	seen := map[string]string{}
	for _, offer := range solved.Offers {
		if offer.DriverID == "d2" {
			t.Errorf("d2 was reserved elsewhere and offered to %s anyway", offer.TripID)
		}
		if other, twice := seen[offer.DriverID]; twice {
			t.Errorf("%s offered to both %s and %s", offer.DriverID, other, offer.TripID)
		}
		seen[offer.DriverID] = offer.TripID
	}
	if len(solved.Offers) != 1 {
		t.Errorf("offers = %+v, want exactly one, for the one free car", solved.Offers)
	}
}

// Every offer carries the positions it was made from, so road time can be
// priced afterwards: one dispatch per offer, from the car to its own rider.
func TestEveryOfferCarriesItsDispatch(t *testing.T) {
	shard := batched()
	withTwoCarsAndTwoRiders(t, shard)
	tick := shard.Tick(base.Add(2 * time.Second))
	solved := shard.Solved(domain.BatchResult{ID: tick.Batch.ID, Err: errors.New("x")}, base.Add(2*time.Second))

	if len(solved.Dispatches) != len(solved.Offers) {
		t.Fatalf("%d offers but %d dispatches", len(solved.Offers), len(solved.Dispatches))
	}
	pickups := map[string]geo.Point{"A": riderA, "B": riderB}
	cars := map[string]geo.Point{"d1": north, "d2": south}
	for _, dispatch := range solved.Dispatches {
		if dispatch.Pickup != pickups[dispatch.TripID] || dispatch.Driver != cars[dispatch.DriverID] {
			t.Errorf("dispatch %+v does not match its trip's pickup and its car's position", dispatch)
		}
	}
}

func TestParseStrategy(t *testing.T) {
	for _, raw := range []string{"greedy", "batched"} {
		if got, err := domain.ParseStrategy(raw); err != nil || string(got) != raw {
			t.Errorf("ParseStrategy(%q) = %q, %v", raw, got, err)
		}
	}
	if _, err := domain.ParseStrategy("hungarian"); err == nil {
		t.Error("an unknown strategy was accepted")
	}
}
