package domain

import (
	"math"
	"slices"
	"sync"

	"github.com/ishakdeveloper/surge/shared/wire"
)

// ETA prediction: how long until the assigned car reaches its rider.
//
// Learned from pickups the fleet actually made — ingest observes them from each
// driver's own status changes — rather than assumed from a speed. A pickup
// takes a fixed overhead plus a pace per straight-line metre:
//
//	seconds = overhead + pace × metres
//
// The overhead is what every pickup pays whatever the distance: taking the
// offer, and up to a ping interval at each end before the status change is
// seen. The pace folds in what a speed leaves out: the detour a road makes, the
// canal in the way.
//
// Both are fitted to absolute error over the last etaWindow pickups: for each
// candidate overhead the pace is the median, and the pair that misses by the
// least wins. Medians, because pickups are long-tailed — one driver stuck
// behind a bridge is not news about the city — and the choice was made on real
// data rather than taste. Replaying 183 pickups from a live run, each predicted
// before it was learned from:
//
//	overhead + median pace   mean 65.5s  median 38.3s  p90 179s
//	naive 30 km/h            mean 69.5s  median 30.6s  p90 197s
//	least squares            mean 71.4s  median 58.2s  p90 166s
//
// Least squares — the version before this one — was the worst of the three,
// pulled around by the tail. Per-cell corrections made every variant worse and
// were dropped. And the margin over naive is small, honestly: straight-line
// speed across those pickups ran from 8 to 40 km/h, most of it the difference
// between one driver and the next, which nothing in a pickup record reveals.
//
// Every observed pickup is still a test before it is a lesson: the model
// predicts it first, beside the naive estimate, and both errors are reported,
// so the comparison keeps running in production rather than living in this
// comment.

const (
	// etaWindow is how many recent pickups the fit is drawn from: enough to be
	// steady, few enough to follow the city through a day.
	etaWindow = 150
	// etaFitMinimum is how many pickups the overhead needs before it is
	// fitted; until then the model is the median pace alone.
	etaFitMinimum = 10
	// etaMaxOverhead and etaOverheadStep bound the overheads tried, in seconds.
	etaMaxOverhead  = 120.0
	etaOverheadStep = 10.0
	// naivePace is the baseline: 30 km/h in a straight line.
	naivePace = 1 / 8.33
	// etaMinMeters ignores pickups that were barely a drive — a driver already
	// at the kerb — which say nothing about driving.
	etaMinMeters = 200.0
)

type pickupSample struct{ meters, seconds float64 }

// EtaModel is the fitted overhead and pace, and the pickups they came from.
type EtaModel struct {
	mu       sync.Mutex
	recent   []pickupSample
	next     int
	overhead float64
	pace     float64
	fitted   bool
}

func NewEtaModel() *EtaModel { return &EtaModel{recent: make([]pickupSample, 0, etaWindow)} }

// Observe scores a pickup against the model and the naive estimate, then learns
// from it. scored is false for a pickup too short to learn from, and until the
// model has anything to predict with.
func (m *EtaModel) Observe(pickup wire.PickupObserved) (learnedError, naiveError float64, scored bool) {
	if pickup.Meters < etaMinMeters || pickup.Seconds <= 0 {
		return 0, 0, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.fitted {
		learnedError = math.Abs(m.overhead + m.pace*pickup.Meters - pickup.Seconds)
		naiveError = math.Abs(pickup.Meters*naivePace - pickup.Seconds)
		scored = true
	}

	sample := pickupSample{meters: pickup.Meters, seconds: pickup.Seconds}
	if len(m.recent) < etaWindow {
		m.recent = append(m.recent, sample)
	} else {
		m.recent[m.next] = sample
		m.next = (m.next + 1) % etaWindow
	}
	m.refit()
	return learnedError, naiveError, scored
}

// Predict is the expected drive, in seconds, over a straight-line distance to
// the pickup; false until any pickup has been observed.
func (m *EtaModel) Predict(meters float64) (float64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.fitted {
		return 0, false
	}
	return m.overhead + m.pace*meters, true
}

func (m *EtaModel) refit() {
	paces := make([]float64, len(m.recent))
	medianPace := func(overhead float64) float64 {
		for i, s := range m.recent {
			paces[i] = (s.seconds - overhead) / s.meters
		}
		slices.Sort(paces)
		return paces[len(paces)/2]
	}

	m.overhead, m.pace, m.fitted = 0, medianPace(0), true
	if len(m.recent) < etaFitMinimum {
		return
	}
	best := math.Inf(1)
	for overhead := 0.0; overhead <= etaMaxOverhead; overhead += etaOverheadStep {
		pace := medianPace(overhead)
		if pace <= 0 {
			continue
		}
		missed := 0.0
		for _, s := range m.recent {
			missed += math.Abs(overhead + pace*s.meters - s.seconds)
		}
		if missed < best {
			best, m.overhead, m.pace = missed, overhead, pace
		}
	}
}
