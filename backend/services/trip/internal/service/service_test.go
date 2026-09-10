package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/trip/internal/domain"
	"github.com/ishakdeveloper/surge/services/trip/internal/infrastructure/repository"
	"github.com/ishakdeveloper/surge/services/trip/internal/service"
	"github.com/ishakdeveloper/surge/shared/geo"
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

// fakeNotifier records every change it is told about, in order.
type fakeNotifier struct {
	changes []domain.Status
}

func (f *fakeNotifier) TripChanged(_ context.Context, trip *domain.Trip) {
	f.changes = append(f.changes, trip.Status)
}

type rig struct {
	service  *service.Service
	router   *fakeRouter
	matcher  *fakeMatcher
	notifier *fakeNotifier
	trips    *repository.InMemory
	now      *time.Time
}

func newRig(t *testing.T, surge float64) *rig {
	t.Helper()

	clock := start
	router := &fakeRouter{}
	matcher := &fakeMatcher{}
	notifier := &fakeNotifier{}
	trips := repository.NewInMemory()

	return &rig{
		service: service.New(service.Options{
			Trips:    trips,
			Fares:    newFareStore(),
			Router:   router,
			Surge:    fakeSurge{multiplier: surge},
			Matcher:  matcher,
			Notifier: notifier,
			Now:      func() time.Time { return clock },
		}),
		router:   router,
		matcher:  matcher,
		notifier: notifier,
		trips:    trips,
		now:      &clock,
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

// Only the driver a trip is assigned to may move it forward, and only through
// the states in order.
func TestDriverLifecycle(t *testing.T) {
	rig := newRig(t, 1.0)
	ctx := context.Background()
	trip := mustCreate(t, rig, "key-lifecycle")

	// Unassigned, nobody may drive it — including a caller with no id, which
	// is the case an unguarded equality check between two empty strings lets
	// through.
	for _, driver := range []string{"drv-1", ""} {
		if _, err := rig.service.Arrive(ctx, trip.ID, driver); !errors.Is(err, service.ErrNotAssigned) {
			t.Errorf("arrive on an unassigned trip as %q: want ErrNotAssigned, got %v", driver, err)
		}
	}

	if _, err := rig.service.Matched(ctx, trip.ID, "drv-1"); err != nil {
		t.Fatalf("matched: %v", err)
	}

	if _, err := rig.service.Arrive(ctx, trip.ID, "drv-2"); !errors.Is(err, service.ErrNotAssigned) {
		t.Errorf("another driver arriving: want ErrNotAssigned, got %v", err)
	}

	// Out of order is refused, not skipped ahead: a rider cannot be "in the
	// car" before the car has reached them.
	if _, err := rig.service.Start(ctx, trip.ID, "drv-1"); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Errorf("start before arrive: want ErrInvalidTransition, got %v", err)
	}

	steps := []struct {
		name string
		do   func(context.Context, string, string) (*domain.Trip, error)
		want domain.Status
	}{
		{"arrive", rig.service.Arrive, domain.StatusArrived},
		{"start", rig.service.Start, domain.StatusInProgress},
		{"complete", rig.service.Complete, domain.StatusCompleted},
	}
	for _, step := range steps {
		got, err := step.do(ctx, trip.ID, "drv-1")
		if err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if got.Status != step.want {
			t.Fatalf("%s: status %s, want %s", step.name, got.Status, step.want)
		}
	}

	// A retried "complete" — the tap that went out twice on a bad connection —
	// returns the same answer rather than a failure the app has to interpret.
	again, err := rig.service.Complete(ctx, trip.ID, "drv-1")
	if err != nil || again.Status != domain.StatusCompleted {
		t.Errorf("a repeated complete should be idempotent: %v, %+v", err, again)
	}
}

// Every change reaches the notifier — cancellation included, which was a
// separate code path with its own copy of read-move-store and no push.
func TestEveryTransitionNotifies(t *testing.T) {
	rig := newRig(t, 1.0)
	ctx := context.Background()
	trip := mustCreate(t, rig, "key-notify")

	if _, err := rig.service.Matched(ctx, trip.ID, "drv-1"); err != nil {
		t.Fatalf("matched: %v", err)
	}
	if _, err := rig.service.Arrive(ctx, trip.ID, "drv-1"); err != nil {
		t.Fatalf("arrive: %v", err)
	}
	if _, err := rig.service.Cancel(ctx, trip.ID, "rider did not show"); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	want := []domain.Status{
		domain.StatusRequested, domain.StatusAccepted, domain.StatusArrived, domain.StatusCancelled,
	}
	if len(rig.notifier.changes) != len(want) {
		t.Fatalf("notified %v, want %v", rig.notifier.changes, want)
	}
	for i := range want {
		if rig.notifier.changes[i] != want[i] {
			t.Errorf("notification %d: %s, want %s", i, rig.notifier.changes[i], want[i])
		}
	}
}

// A refused change is not news, and pushing it would tell a rider their trip
// moved when it did not.
func TestRefusedTransitionIsNotNotified(t *testing.T) {
	rig := newRig(t, 1.0)
	ctx := context.Background()
	trip := mustCreate(t, rig, "key-refused")
	before := len(rig.notifier.changes)

	if _, err := rig.service.Arrive(ctx, trip.ID, "drv-9"); err == nil {
		t.Fatal("an unassigned driver arrived")
	}
	if len(rig.notifier.changes) != before {
		t.Errorf("a refused transition was pushed: %v", rig.notifier.changes)
	}
}

// A driver reloading mid-ride finds the trip they are on, and nobody else's.
func TestDriverListsTheirOwnTrips(t *testing.T) {
	rig := newRig(t, 1.0)
	ctx := context.Background()

	mine := mustCreate(t, rig, "key-mine")
	if _, err := rig.service.Matched(ctx, mine.ID, "drv-1"); err != nil {
		t.Fatalf("matched: %v", err)
	}
	theirs := mustCreate(t, rig, "key-theirs")
	if _, err := rig.service.Matched(ctx, theirs.ID, "drv-2"); err != nil {
		t.Fatalf("matched: %v", err)
	}
	mustCreate(t, rig, "key-open")

	page, err := rig.service.List(ctx, domain.ListFilter{DriverID: "drv-1"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Trips) != 1 || page.Trips[0].ID != mine.ID {
		t.Errorf("drv-1 listed %d trips, want only %s", len(page.Trips), mine.ID)
	}

	// A filter built wrongly — no owner at all — lists nothing rather than
	// everything. That is the only acceptable way for that bug to fail.
	empty, err := rig.service.List(ctx, domain.ListFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(empty.Trips) != 0 {
		t.Errorf("an ownerless filter listed %d trips", len(empty.Trips))
	}
}
