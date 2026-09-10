package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/trip/internal/domain"
)

var now = time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

// The state machine is the part of a trip worth being sure about, and it is
// testable by calling functions precisely because the domain imports no
// transport or storage.
func TestTransitions(t *testing.T) {
	legal := []struct{ from, to domain.Status }{
		{domain.StatusRequested, domain.StatusOffered},
		// The matcher reports outcomes, not every offer it makes.
		{domain.StatusRequested, domain.StatusAccepted},
		{domain.StatusOffered, domain.StatusAccepted},
		{domain.StatusAccepted, domain.StatusArrived},
		{domain.StatusArrived, domain.StatusInProgress},
		{domain.StatusInProgress, domain.StatusCompleted},
		// A declined offer returns the trip to the pool rather than failing it.
		{domain.StatusOffered, domain.StatusRequested},
		// And an unmatched trip can be retried.
		{domain.StatusUnmatched, domain.StatusRequested},
	}

	for _, move := range legal {
		trip := &domain.Trip{Status: move.from}
		if err := trip.Transition(move.to, now); err != nil {
			t.Errorf("%s -> %s should be legal: %v", move.from, move.to, err)
		}
	}

	illegal := []struct{ from, to domain.Status }{
		// Skipping the driver arriving.
		{domain.StatusAccepted, domain.StatusInProgress},
		// A completed trip is finished.
		{domain.StatusCompleted, domain.StatusCancelled},
		{domain.StatusCompleted, domain.StatusInProgress},
		// Cancelling is terminal too.
		{domain.StatusCancelled, domain.StatusAccepted},
		// A rider cannot be picked up before anyone accepted.
		{domain.StatusRequested, domain.StatusInProgress},
	}

	for _, move := range illegal {
		trip := &domain.Trip{Status: move.from}
		if err := trip.Transition(move.to, now); !errors.Is(err, domain.ErrInvalidTransition) {
			t.Errorf("%s -> %s should be refused, got %v", move.from, move.to, err)
		}
	}
}

// At-least-once delivery means the same event arrives twice. Asking a trip to
// enter the state it is already in must be a no-op, not an error, or every
// redelivery becomes noise.
func TestRepeatedTransitionIsANoOp(t *testing.T) {
	trip := &domain.Trip{Status: domain.StatusAccepted, UpdatedAt: now}

	if err := trip.Transition(domain.StatusAccepted, now.Add(time.Minute)); err != nil {
		t.Fatalf("a repeated transition should be accepted silently: %v", err)
	}
	if trip.UpdatedAt != now {
		t.Error("a no-op transition moved the updated timestamp")
	}
}

func TestTerminal(t *testing.T) {
	for _, status := range []domain.Status{domain.StatusCompleted, domain.StatusCancelled} {
		if !(&domain.Trip{Status: status}).Terminal() {
			t.Errorf("%s should be terminal", status)
		}
	}
	// Unmatched is NOT terminal: the rider can ask again.
	if (&domain.Trip{Status: domain.StatusUnmatched}).Terminal() {
		t.Error("unmatched should allow a retry")
	}
}

func TestQuote(t *testing.T) {
	sedan, err := domain.PackageBySlug("sedan")
	if err != nil {
		t.Fatalf("sedan: %v", err)
	}

	// 6.84 km / 721 s — the real Centraal to Rijksmuseum route.
	fare := sedan.Quote(6840, 721, 1.0)
	if fare < 1000 || fare > 2500 {
		t.Errorf("a 6.8 km city trip priced at %d cents, which is not plausible", fare)
	}

	// Surge multiplies the metered fare.
	surged := sedan.Quote(6840, 721, 2.0)
	if surged <= fare {
		t.Error("surge did not raise the fare")
	}

	// A trip round the corner falls to the minimum rather than to nothing.
	tiny := sedan.Quote(300, 60, 1.0)
	if tiny != sedan.MinimumCents {
		t.Errorf("a 300 m trip priced at %d, want the %d minimum", tiny, sedan.MinimumCents)
	}

	// Surge still applies to a short trip — demand is demand — but the floor is
	// never itself multiplied. Asserting equality with the minimum here was
	// wrong: it would have meant short trips were exempt from surge entirely.
	tinySurged := sedan.Quote(300, 60, 3.0)
	if tinySurged < tiny {
		t.Errorf("surge lowered a short fare: %d < %d", tinySurged, tiny)
	}
	if tinySurged >= sedan.MinimumCents*3 {
		t.Errorf("the minimum itself was multiplied: %d is 3x the %d floor", tinySurged, sedan.MinimumCents)
	}
}
