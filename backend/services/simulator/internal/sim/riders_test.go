package sim_test

import (
	"strings"
	"testing"

	"github.com/ishakdeveloper/surge/services/simulator/internal/sim"
)

// The same seed and sequence in two runs are two different trips. The seed
// still reproduces the demand; it no longer reproduces the names, which are
// idempotency keys a long-lived matcher remembers.
func TestTripIDsDifferAcrossRuns(t *testing.T) {
	first := sim.TripID(1, 1789000000000000000, 42)
	second := sim.TripID(1, 1789000000123456789, 42)
	if first == second {
		t.Fatalf("two runs named a trip the same: %s", first)
	}
	if again := sim.TripID(1, 1789000000000000000, 42); again != first {
		t.Errorf("TripID is not deterministic: %s then %s", first, again)
	}
	if !strings.HasPrefix(first, "trip-1-") || !strings.HasSuffix(first, "-42") {
		t.Errorf("%s does not read as seed, run, sequence", first)
	}
}
