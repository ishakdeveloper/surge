package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/trip/internal/domain"
	"github.com/ishakdeveloper/surge/services/trip/internal/infrastructure/repository"
	"github.com/ishakdeveloper/surge/services/trip/internal/service"
)

func kinds(facts []domain.Fact) []domain.FactKind {
	out := make([]domain.FactKind, len(facts))
	for i, fact := range facts {
		out[i] = fact.Kind
	}
	return out
}

func assertKinds(t *testing.T, got []domain.Fact, want ...domain.FactKind) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("facts %v, want %v", kinds(got), want)
	}
	for i := range want {
		if got[i].Kind != want[i] {
			t.Fatalf("facts %v, want %v", kinds(got), want)
		}
	}
}

// The facts payments and chat act on: one request per booking however often it
// is retried, one acceptance however often the match is redelivered, one
// completion however often the driver taps, and nothing for the moves in
// between that only the people on the trip care about.
func TestFactsTravelWithTheirChange(t *testing.T) {
	rig := newRig(t, 1.0)
	ctx := context.Background()

	trip := mustCreate(t, rig, "key-facts")
	// The retried tap. The same booking, not a second hold on the card.
	if again := mustCreate(t, rig, "key-facts"); again.ID != trip.ID {
		t.Fatalf("a retry booked a second trip")
	}

	if _, err := rig.service.Matched(ctx, trip.ID, "drv-1"); err != nil {
		t.Fatalf("matched: %v", err)
	}
	// Redelivered by the broker.
	if _, err := rig.service.Matched(ctx, trip.ID, "drv-1"); err != nil {
		t.Fatalf("redelivered match: %v", err)
	}
	for _, step := range []func(context.Context, string, string) (*domain.Trip, error){
		rig.service.Arrive, rig.service.Start, rig.service.Complete, rig.service.Complete,
	} {
		if _, err := step(ctx, trip.ID, "drv-1"); err != nil {
			t.Fatalf("driver step: %v", err)
		}
	}

	facts := rig.trips.Facts()
	assertKinds(t, facts, domain.FactRequested, domain.FactAccepted, domain.FactCompleted)

	if accepted := facts[1].Trip; accepted.DriverID != "drv-1" {
		t.Errorf("acceptance names driver %q, want drv-1 — there is nobody to talk to", accepted.DriverID)
	}

	completed := facts[2].Trip
	if completed.DriverID != "drv-1" {
		t.Errorf("completion names driver %q, want drv-1 — there is nobody to pay", completed.DriverID)
	}
	if completed.TotalCents != trip.TotalCents || completed.Currency != domain.MarketCurrency {
		t.Errorf("completion carries %d %s, booked at %d %s",
			completed.TotalCents, completed.Currency, trip.TotalCents, domain.MarketCurrency)
	}
}

func TestCancelKeepsItsFirstReason(t *testing.T) {
	rig := newRig(t, 1.0)
	ctx := context.Background()
	trip := mustCreate(t, rig, "key-reason")

	if _, err := rig.service.Cancel(ctx, trip.ID, "payment_failed"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := rig.service.Cancel(ctx, trip.ID, "rider"); err != nil {
		t.Fatalf("repeated cancel: %v", err)
	}

	stored, err := rig.trips.Get(ctx, trip.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.CancelReason != "payment_failed" {
		t.Errorf("reason %q, want the first one", stored.CancelReason)
	}

	facts := rig.trips.Facts()
	assertKinds(t, facts, domain.FactRequested, domain.FactCancelled)
	if facts[1].Trip.CancelReason != "payment_failed" {
		t.Errorf("the cancel fact says %q", facts[1].Trip.CancelReason)
	}
}

// A match naming a second driver must not move the money to them.
func TestASecondMatchCannotReassign(t *testing.T) {
	rig := newRig(t, 1.0)
	ctx := context.Background()
	trip := mustCreate(t, rig, "key-reassign")

	if _, err := rig.service.Matched(ctx, trip.ID, "drv-1"); err != nil {
		t.Fatalf("matched: %v", err)
	}
	if _, err := rig.service.Matched(ctx, trip.ID, "drv-2"); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("a second driver: want ErrInvalidTransition, got %v", err)
	}

	stored, _ := rig.trips.Get(ctx, trip.ID)
	if stored.DriverID != "drv-1" {
		t.Errorf("trip reassigned to %q", stored.DriverID)
	}
}

// racingRepository lets a competing write land between a transition's read
// and its write — the window the compare-and-set exists for.
type racingRepository struct {
	*repository.InMemory
	race func()
}

func (r *racingRepository) Update(ctx context.Context, trip *domain.Trip, from domain.Status, facts ...domain.Fact) error {
	if race := r.race; race != nil {
		r.race = nil
		race()
	}
	return r.InMemory.Update(ctx, trip, from, facts...)
}

// A rider cancels at the instant the matcher accepts. Before the
// compare-and-set, whichever write landed second won: here, a cancelled trip
// with a driver on the way. Now the losing accept re-reads, finds the trip
// cancelled, and is refused.
func TestAnAcceptThatLosesToACancelIsRefused(t *testing.T) {
	ctx := context.Background()
	trips := &racingRepository{InMemory: repository.NewInMemory()}
	trip := service.New(service.Options{
		Trips:   trips,
		Fares:   newFareStore(),
		Router:  &fakeRouter{},
		Surge:   fakeSurge{multiplier: 1},
		Matcher: &fakeMatcher{},
		Now:     func() time.Time { return start },
	})

	fares, _, err := trip.Preview(ctx, "rider-1", pickup, dropoff)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	booked, err := trip.Create(ctx, "rider-1", fares[0].ID, "key-race")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	trips.race = func() {
		if _, err := trip.Cancel(ctx, booked.ID, "rider"); err != nil {
			t.Fatalf("the racing cancel: %v", err)
		}
	}

	if _, err := trip.Matched(ctx, booked.ID, "drv-1"); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("the losing accept: want ErrInvalidTransition, got %v", err)
	}

	stored, _ := trips.Get(ctx, booked.ID)
	if stored.Status != domain.StatusCancelled || stored.DriverID != "" {
		t.Errorf("stored %s with driver %q, want cancelled with none", stored.Status, stored.DriverID)
	}
	assertKinds(t, trips.Facts(), domain.FactRequested, domain.FactCancelled)
}
