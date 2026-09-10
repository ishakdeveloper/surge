package domain

import (
	"fmt"
	"math"
	"time"

	"github.com/ishakdeveloper/surge/shared/geo"
)

// Batched matching.
//
// Greedy matching gives each request the nearest free driver the moment it
// arrives, which is locally right and globally wrong: two riders a street
// apart, two cars, and the first rider takes the car the second one needed
// while the other car drives twice as far. Batching gathers a window of
// requests and solves them together, minimising total pickup time.
//
// The shard stays pure. Travel times come from Valhalla, which is I/O, so the
// shard asks for them in its Outcome and the runner answers later through
// Solved — one more message into the same single-threaded loop, like a Kafka
// record. Between the two the world moves on, and a driver chosen by the solve
// may be taken by the time the answer lands. What already handles that for
// greedy handles it here: the chosen driver becomes the request's first
// candidate, and advance re-checks availability before reserving. A batch only
// ever decides who to try first; it never goes around the checks.

// Strategy is how a shard matches.
type Strategy string

const (
	// StrategyGreedy offers each request its nearest free driver on arrival.
	StrategyGreedy Strategy = "greedy"
	// StrategyBatched gathers requests for BatchWindow and solves them
	// together over travel times.
	StrategyBatched Strategy = "batched"
)

func ParseStrategy(raw string) (Strategy, error) {
	switch Strategy(raw) {
	case StrategyGreedy, StrategyBatched:
		return Strategy(raw), nil
	default:
		return "", fmt.Errorf("matcher: unknown strategy %q, want greedy or batched", raw)
	}
}

// BatchRequest asks the runner for travel times from each driver to each
// pickup.
type BatchRequest struct {
	ID      uint64
	Drivers []geo.Point
	Pickups []geo.Point
}

// BatchResult answers a BatchRequest. Seconds is drivers × pickups, with +Inf
// for a pair Valhalla found no road between. Err means there are no travel
// times at all, and the shard ranks by straight-line distance instead of
// holding its riders until the router comes back.
type BatchResult struct {
	ID      uint64
	Seconds [][]float64
	Err     error
}

type batch struct {
	id      uint64
	trips   []string
	pickups []geo.Point
	drivers []string
	points  []geo.Point
	// eligible is, per trip, the drivers that were its candidates. Anyone else
	// in the batch is too far away for that rider and costs +Inf.
	eligible []map[string]struct{}
	since    time.Time
}

// crowFliesSpeed turns straight-line metres into seconds when there are no
// travel times: 30 km/h, a city average, and only ever used to rank.
const crowFliesSpeed = 8.3

func (s *Shard) queue(tripID string, now time.Time) {
	if len(s.waiting) == 0 {
		s.windowOpened = now
	}
	s.waiting = append(s.waiting, tripID)
}

// maybeBatch closes the window once it has been open long enough, and asks for
// the travel times to solve it. One batch in flight at a time: requests that
// arrive meanwhile wait for the next window, which keeps a slow router from
// piling solves up behind each other.
func (s *Shard) maybeBatch(now time.Time) Outcome {
	var outcome Outcome

	if s.inflight != nil {
		if now.Sub(s.inflight.since) < s.config.BatchTimeout {
			return outcome
		}
		// The answer is not coming. Solve over distance rather than leave
		// these riders waiting on a router that has gone quiet.
		stale := s.inflight
		s.inflight = nil
		outcome.merge(s.resolve(stale, nil, now))
	}

	if len(s.waiting) == 0 || now.Sub(s.windowOpened) < s.config.BatchWindow {
		return outcome
	}

	take := min(len(s.waiting), s.config.BatchMaxRequests)
	trips := s.waiting[:take]
	s.waiting = append([]string(nil), s.waiting[take:]...)
	if len(s.waiting) > 0 {
		s.windowOpened = now
	}

	s.batchSeq++
	b := &batch{id: s.batchSeq, since: now}
	seen := map[string]bool{}
	// Every rider gets a share of the driver budget, so one request with many
	// nearby cars cannot crowd everybody else out of the solve.
	share := max(2, s.config.BatchMaxDrivers/take)

	for _, tripID := range trips {
		request, live := s.pending[tripID]
		if !live || request.outstanding != "" {
			continue
		}

		// Ranked now, not on arrival: two seconds is a hundred metres of
		// driving, and a candidate list that old is somebody else's list.
		pickup := geo.Point{Lat: request.payload.PickupLat, Lng: request.payload.PickupLng}
		candidates, err := s.rank(pickup, now)
		if err != nil || len(candidates) == 0 {
			delete(s.pending, tripID)
			outcome.Abandoned = append(outcome.Abandoned, tripID)
			continue
		}
		request.candidates, request.next = candidates, 0

		eligible := map[string]struct{}{}
		for _, c := range candidates {
			if !c.ownedHere || len(eligible) >= share {
				continue
			}
			if !seen[c.driverID] {
				if len(b.drivers) >= s.config.BatchMaxDrivers {
					continue
				}
				seen[c.driverID] = true
				b.drivers = append(b.drivers, c.driverID)
				b.points = append(b.points, s.drivers[c.driverID].point)
			}
			eligible[c.driverID] = struct{}{}
		}

		b.trips = append(b.trips, tripID)
		b.pickups = append(b.pickups, pickup)
		b.eligible = append(b.eligible, eligible)
	}

	if len(b.trips) == 0 {
		return outcome
	}
	if len(b.drivers) == 0 {
		// Every candidate is across a shard boundary. There is nothing local to
		// solve over, and the greedy path is the one that knows how to chase a
		// driver into another shard.
		outcome.merge(s.resolve(b, nil, now))
		return outcome
	}

	s.inflight = b
	outcome.Batch = &BatchRequest{ID: b.id, Drivers: b.points, Pickups: b.pickups}
	return outcome
}

// Solved takes the travel times a BatchRequest asked for and dispatches the
// batch. A result for any batch other than the one in flight is ignored: it
// arrived after a timeout had already dispatched that batch.
func (s *Shard) Solved(result BatchResult, now time.Time) Outcome {
	if s.inflight == nil || s.inflight.id != result.ID {
		return Outcome{}
	}
	b := s.inflight
	s.inflight = nil

	seconds := result.Seconds
	if result.Err != nil || len(seconds) != len(b.drivers) {
		seconds = nil
	}
	return s.resolve(b, seconds, now)
}

// resolve assigns the batch and dispatches it. Without travel times it ranks by
// distance, which is still a joint solve and still better than greedy.
func (s *Shard) resolve(b *batch, seconds [][]float64, now time.Time) Outcome {
	cost := make([][]float64, len(b.trips))
	for i := range b.trips {
		cost[i] = make([]float64, len(b.drivers))
		for j, driverID := range b.drivers {
			_, eligible := b.eligible[i][driverID]
			switch {
			case !eligible:
				cost[i][j] = math.Inf(1)
			case seconds != nil && i < len(seconds[j]):
				cost[i][j] = seconds[j][i]
			default:
				cost[i][j] = geo.DistanceMeters(b.points[j], b.pickups[i]) / crowFliesSpeed
			}
		}
	}
	assigned := Assign(cost)

	// Assigned riders are dispatched first, so one the solve left without a
	// car cannot take a car the solve gave to somebody else.
	order := make([]int, 0, len(b.trips))
	for i, j := range assigned {
		if j >= 0 {
			order = append(order, i)
		}
	}
	for i, j := range assigned {
		if j < 0 {
			order = append(order, i)
		}
	}

	var outcome Outcome
	for _, i := range order {
		request, live := s.pending[b.trips[i]]
		if !live || request.outstanding != "" {
			continue
		}
		if j := assigned[i]; j >= 0 {
			request.prefer(b.drivers[j])
		}
		outcome.merge(s.advance(request, now))
	}
	return outcome
}

// prefer moves a driver to the front of the candidates still to be tried.
func (r *pendingRequest) prefer(driverID string) {
	for k := r.next; k < len(r.candidates); k++ {
		if r.candidates[k].driverID != driverID {
			continue
		}
		chosen := r.candidates[k]
		copy(r.candidates[r.next+1:k+1], r.candidates[r.next:k])
		r.candidates[r.next] = chosen
		return
	}
}

func (o *Outcome) merge(other Outcome) {
	o.GeoEvents = append(o.GeoEvents, other.GeoEvents...)
	o.Offers = append(o.Offers, other.Offers...)
	o.Matched = append(o.Matched, other.Matched...)
	o.Abandoned = append(o.Abandoned, other.Abandoned...)
	o.Rejections = append(o.Rejections, other.Rejections...)
	o.Dispatches = append(o.Dispatches, other.Dispatches...)
	if other.Batch != nil {
		o.Batch = other.Batch
	}
}
