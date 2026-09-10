package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/internal/trip/domain"
	"github.com/ishakdeveloper/surge/internal/trip/infrastructure/repository"
	"github.com/ishakdeveloper/surge/internal/trip/service"
	"github.com/ishakdeveloper/surge/pkg/geo"
)

var (
	pickup  = geo.Point{Lat: 52.3791, Lng: 4.9003}
	dropoff = geo.Point{Lat: 52.3600, Lng: 4.8852}
	start   = time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
)

// Fakes rather than mocks: the interfaces are small enough that a struct with a
// counter says more than a mocking framework would.
type fakeRouter struct{ calls int }

func (f *fakeRouter) Route(context.Context, geo.Point, geo.Point) (service.RouteResult, error) {
	f.calls++
	return service.RouteResult{Polyline6: "abc", Meters: 6840, Seconds: 721}, nil
}

type fakeSurge struct{ multiplier float64 }

func (f fakeSurge) MultiplierAt(context.Context, geo.Point) float64 { return f.multiplier }

type fakeMatcher struct {
	mu    sync.Mutex
	trips []string
}

func (f *fakeMatcher) RequestMatch(_ context.Context, trip *domain.Trip) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.trips = append(f.trips, trip.ID)
	return nil
}

type fareStore struct {
	mu    sync.Mutex
	fares map[string]domain.Fare
}

func newFareStore() *fareStore { return &fareStore{fares: map[string]domain.Fare{}} }

func (f *fareStore) Put(_ context.Context, fare domain.Fare) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fares[fare.ID] = fare
	return nil
}

func (f *fareStore) Get(_ context.Context, id string) (domain.Fare, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fare, ok := f.fares[id]
	if !ok {
		return domain.Fare{}, service.ErrFareNotFound
	}
	return fare, nil
}

type rig struct {
	service *service.Service
	router  *fakeRouter
	matcher *fakeMatcher
	trips   *repository.InMemory
	now     *time.Time
}

func newRig(t *testing.T, surge float64) *rig {
	t.Helper()

	clock := start
	router := &fakeRouter{}
	matcher := &fakeMatcher{}
	trips := repository.NewInMemory()

	return &rig{
		service: service.New(service.Options{
			Trips:   trips,
			Fares:   newFareStore(),
			Router:  router,
			Surge:   fakeSurge{multiplier: surge},
			Matcher: matcher,
			Now:     func() time.Time { return clock },
		}),
		router:  router,
		matcher: matcher,
		trips:   trips,
		now:     &clock,
	}
}

// One route call for every vehicle class: the road is the same whichever car
// drives it, and a rider dragging a pin would otherwise cost three times as
// much routing for an identical answer.
func TestPreviewRoutesOnce(t *testing.T) {
	rig := newRig(t, 1.0)

	fares, route, err := rig.service.Preview(context.Background(), "rider-1", pickup, dropoff)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}

	if len(fares) != len(domain.Catalogue) {
		t.Errorf("got %d quotes for %d classes", len(fares), len(domain.Catalogue))
	}
	if rig.router.calls != 1 {
		t.Errorf("routed %d times for one preview", rig.router.calls)
	}
	if route.Meters != 6840 {
		t.Errorf("route not returned intact: %+v", route)
	}
}

// The property the whole idempotency key exists for: a rider on a flaky
// connection who taps twice gets one trip and one driver.
func TestCreateIsIdempotent(t *testing.T) {
	rig := newRig(t, 1.0)
	ctx := context.Background()

	fares, _, err := rig.service.Preview(ctx, "rider-1", pickup, dropoff)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}

	first, err := rig.service.Create(ctx, "rider-1", fares[0].ID, "key-abc")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	second, err := rig.service.Create(ctx, "rider-1", fares[0].ID, "key-abc")
	if err != nil {
		t.Fatalf("retry: %v", err)
	}

	if first.ID != second.ID {
		t.Fatalf("a retry booked a second trip: %s then %s", first.ID, second.ID)
	}
	if len(rig.matcher.trips) != 1 {
		t.Fatalf("the matcher was asked for %d drivers, want 1", len(rig.matcher.trips))
	}
}

// A rider who saw one price must not be charged another. Re-quoting silently
// would be the convenient behaviour and the dishonest one.
func TestExpiredFareIsRefusedNotRequoted(t *testing.T) {
	rig := newRig(t, 1.0)
	ctx := context.Background()

	fares, _, err := rig.service.Preview(ctx, "rider-1", pickup, dropoff)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}

	*rig.now = start.Add(service.FareTTL + time.Second)

	if _, err := rig.service.Create(ctx, "rider-1", fares[0].ID, "key-abc"); !errors.Is(err, service.ErrFareExpired) {
		t.Fatalf("want ErrFareExpired, got %v", err)
	}
	if len(rig.matcher.trips) != 0 {
		t.Error("an expired fare still asked for a driver")
	}
}

// The quoted price is what the trip carries. If booking re-derived it, a surge
// change between preview and tap would move the number the rider agreed to.
func TestBookedTripKeepsTheQuotedPrice(t *testing.T) {
	rig := newRig(t, 2.5)
	ctx := context.Background()

	fares, _, err := rig.service.Preview(ctx, "rider-1", pickup, dropoff)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}

	quoted := fares[0]
	trip, err := rig.service.Create(ctx, "rider-1", quoted.ID, "key-abc")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if trip.TotalCents != quoted.TotalCents {
		t.Errorf("booked at %d, quoted %d", trip.TotalCents, quoted.TotalCents)
	}
	if trip.SurgeMultiplier != 2.5 {
		t.Errorf("surge multiplier %v not carried onto the trip", trip.SurgeMultiplier)
	}
	// And the route travels with it, so nothing re-routes on accept.
	if trip.Polyline6 != quoted.Polyline6 {
		t.Error("the trip does not carry the route it was quoted from")
	}
}

func TestMatchedAndUnmatched(t *testing.T) {
	rig := newRig(t, 1.0)
	ctx := context.Background()

	fares, _, _ := rig.service.Preview(ctx, "rider-1", pickup, dropoff)
	trip, err := rig.service.Create(ctx, "rider-1", fares[0].ID, "key-abc")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	matched, err := rig.service.Matched(ctx, trip.ID, "drv-1")
	if err != nil {
		t.Fatalf("matched: %v", err)
	}
	if matched.Status != domain.StatusAccepted || matched.DriverID != "drv-1" {
		t.Fatalf("unexpected state after matching: %+v", matched)
	}

	// Redelivery: the same event again must be a no-op, not an error.
	if _, err := rig.service.Matched(ctx, trip.ID, "drv-1"); err != nil {
		t.Errorf("a redelivered match should be silent: %v", err)
	}

	// And an unmatched event for an already-accepted trip must be refused
	// rather than quietly undoing the assignment.
	if _, err := rig.service.Unmatched(ctx, trip.ID); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Errorf("want ErrInvalidTransition, got %v", err)
	}
}

// mustCreate books a second trip for tests that need one.
func mustCreate(t *testing.T, rig *rig, key string) *domain.Trip {
	t.Helper()

	ctx := context.Background()
	fares, _, err := rig.service.Preview(ctx, "rider-1", pickup, dropoff)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}

	trip, err := rig.service.Create(ctx, "rider-1", fares[0].ID, key)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return trip
}

func TestCancel(t *testing.T) {
	rig := newRig(t, 1.0)
	ctx := context.Background()

	fares, _, _ := rig.service.Preview(ctx, "rider-1", pickup, dropoff)
	trip, _ := rig.service.Create(ctx, "rider-1", fares[0].ID, "key-abc")

	cancelled, err := rig.service.Cancel(ctx, trip.ID, "changed my mind")
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if cancelled.Status != domain.StatusCancelled {
		t.Errorf("status %s, want cancelled", cancelled.Status)
	}

	// Cancelling twice is idempotent, not an error. This is an RPC a phone
	// makes, so a retried tap must return the same answer rather than a
	// failure the app has to interpret.
	again, err := rig.service.Cancel(ctx, trip.ID, "again")
	if err != nil {
		t.Errorf("a repeated cancel should be idempotent: %v", err)
	}
	if again.ID != cancelled.ID || again.Status != domain.StatusCancelled {
		t.Error("a repeated cancel returned something other than the cancelled trip")
	}

	// Cancelling a trip that already finished is a different matter: the ride
	// happened, and the caller is acting on state from before it did.
	finished, _ := rig.service.Matched(ctx, mustCreate(t, rig, "key-two").ID, "drv-2")
	for _, next := range []domain.Status{domain.StatusArrived, domain.StatusInProgress, domain.StatusCompleted} {
		if err := finished.Transition(next, start); err != nil {
			t.Fatalf("advancing to %s: %v", next, err)
		}
	}
	if err := rig.trips.Update(ctx, finished); err != nil {
		t.Fatalf("update: %v", err)
	}

	if _, err := rig.service.Cancel(ctx, finished.ID, "too late"); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Errorf("cancelling a completed trip should be refused, got %v", err)
	}
}
