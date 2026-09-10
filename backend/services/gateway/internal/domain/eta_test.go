package domain_test

import (
	"bufio"
	"encoding/json"
	"math"
	"math/rand/v2"
	"os"
	"slices"
	"testing"

	"github.com/ishakdeveloper/surge/services/gateway/internal/domain"
	"github.com/ishakdeveloper/surge/shared/wire"
)

func pickupOf(meters, seconds float64) wire.PickupObserved {
	return wire.PickupObserved{Tag: wire.TagPickupObserved, Cell: "871969c9bffffff", Meters: meters, Seconds: seconds}
}

func TestNoPredictionBeforeAnyPickup(t *testing.T) {
	if _, ok := domain.NewEtaModel().Predict(1000); ok {
		t.Error("predicted with nothing learned")
	}
}

// A city whose pickups run at 0.2 seconds per metre predicts a kilometre and a
// half at about 300 seconds.
func TestTheModelLearnsTheCitysPace(t *testing.T) {
	model := domain.NewEtaModel()
	for range 20 {
		model.Observe(pickupOf(1000, 200))
	}
	if seconds, ok := model.Predict(1500); !ok || math.Abs(seconds-300) > 1 {
		t.Errorf("1.5 km = %.0fs, want about 300", seconds)
	}
}

// Pickups that each pay a fixed half minute on top of the drive: overhead plus
// pace fits that shape, and beats a fixed 30 km/h guess.
func TestOverheadAndPaceBeatANaiveGuess(t *testing.T) {
	model := domain.NewEtaModel()
	source := rand.New(rand.NewPCG(5, 8))
	pickup := func() wire.PickupObserved {
		meters := 250 + source.Float64()*2250
		return pickupOf(meters, 30+meters*0.09+source.NormFloat64()*8)
	}
	for range 200 {
		model.Observe(pickup())
	}

	var learned, naive float64
	for range 200 {
		l, n, scored := model.Observe(pickup())
		if !scored {
			t.Fatal("a pickup went unscored with a trained model")
		}
		learned, naive = learned+l, naive+n
	}
	learned, naive = learned/200, naive/200
	if learned >= naive || learned > 12 {
		t.Errorf("mean error: learned %.1fs, naive %.1fs; want learned near the 8s noise and below naive", learned, naive)
	}
}

// One driver stuck behind a bridge is not news about the city. A median barely
// moves for a few wild pickups where a mean would be dragged along.
func TestAFewSlowOutliersBarelyMoveThePrediction(t *testing.T) {
	model := domain.NewEtaModel()
	for range 50 {
		model.Observe(pickupOf(1000, 200))
	}
	for range 3 {
		model.Observe(pickupOf(1000, 2000))
	}
	if seconds, _ := model.Predict(1000); math.Abs(seconds-200) > 10 {
		t.Errorf("after three outliers a kilometre = %.0fs, want still about 200", seconds)
	}
}

// Every pickup is scored before it is learned from.
func TestEachPickupIsScoredBeforeItIsLearned(t *testing.T) {
	model := domain.NewEtaModel()
	if _, _, scored := model.Observe(pickupOf(1000, 250)); scored {
		t.Error("the first pickup was scored against a model that knew nothing")
	}
	if _, _, scored := model.Observe(pickupOf(1200, 300)); !scored {
		t.Error("the second pickup went unscored")
	}
}

// A driver already at the kerb teaches nothing about driving.
func TestAPickupThatWasBarelyADriveIsIgnored(t *testing.T) {
	model := domain.NewEtaModel()
	model.Observe(pickupOf(30, 40))
	if _, ok := model.Predict(1000); ok {
		t.Error("learned from a 30 m pickup")
	}
}

// The pickups of a live run — 300 drivers doing 1.5 km trips, most demand
// around three hotspots — replayed in order, each predicted before it is
// learned from. The model must beat the naive guess on the mean and the tail.
// It does not on the median, and this test does not pretend it does: see the
// comment on the model.
func TestOnRealPickupsTheModelBeatsANaiveGuess(t *testing.T) {
	file, err := os.Open("testdata/pickups.jsonl")
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	defer file.Close()

	var pickups []wire.PickupObserved
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var pickup wire.PickupObserved
		if json.Unmarshal(scanner.Bytes(), &pickup) == nil && pickup.Tag == wire.TagPickupObserved {
			pickups = append(pickups, pickup)
		}
	}
	slices.SortFunc(pickups, func(a, b wire.PickupObserved) int { return int(a.AtMs - b.AtMs) })

	model := domain.NewEtaModel()
	var learned, naive []float64
	for i, pickup := range pickups {
		l, n, scored := model.Observe(pickup)
		if scored && i >= 20 {
			learned, naive = append(learned, l), append(naive, n)
		}
	}
	if len(learned) < 100 {
		t.Fatalf("only %d pickups scored; the fixture is too small to judge", len(learned))
	}
	mean := func(v []float64) float64 {
		total := 0.0
		for _, x := range v {
			total += x
		}
		return total / float64(len(v))
	}
	p90 := func(v []float64) float64 {
		sorted := slices.Clone(v)
		slices.Sort(sorted)
		return sorted[len(sorted)*9/10]
	}
	t.Logf("%d real pickups: learned mean %.1fs p90 %.0fs; naive mean %.1fs p90 %.0fs",
		len(learned), mean(learned), p90(learned), mean(naive), p90(naive))
	if mean(learned) >= mean(naive) || p90(learned) >= p90(naive) {
		t.Errorf("the learned model did not beat the naive guess on real pickups")
	}
}
