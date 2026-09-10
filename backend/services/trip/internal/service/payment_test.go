package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/trip/internal/domain"
	"github.com/ishakdeveloper/surge/services/trip/internal/infrastructure/repository"
	"github.com/ishakdeveloper/surge/services/trip/internal/service"
)

// gatedRig is a trip service that waits for payments before dispatching.
func gatedRig(t *testing.T) *rig {
	t.Helper()

	clock := start
	router := &fakeRouter{}
	matcher := &fakeMatcher{}
	notifier := &fakeNotifier{}
	trips := repository.NewInMemory()

	return &rig{
		service: service.New(service.Options{
			Trips: trips, Fares: newFareStore(), Router: router, Surge: fakeSurge{multiplier: 1},
			Matcher: matcher, Notifier: notifier, RequirePayment: true,
			Now: func() time.Time { return clock },
		}),
		router: router, matcher: matcher, notifier: notifier, trips: trips, now: &clock,
	}
}

func (r *rig) status(t *testing.T, id string) *domain.Trip {
	t.Helper()
	trip, err := r.trips.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	return trip
}

// Nobody is sent to a ride that cannot be paid for: the matcher hears about a
// booking only once its fare is held.
func TestABookingWaitsForItsPayment(t *testing.T) {
	rig := gatedRig(t)
	ctx := context.Background()

	trip := mustCreate(t, rig, "key-gated")
	if trip.Status != domain.StatusPaymentPending {
		t.Fatalf("booked as %s, want payment_pending", trip.Status)
	}
	if len(rig.matcher.trips) != 0 {
		t.Fatal("the matcher was asked before the fare was held")
	}
	// The request payments holds on is still stored with the booking.
	assertKinds(t, rig.trips.Facts(), domain.FactRequested)

	if err := rig.service.PaymentAuthorized(ctx, trip.ID); err != nil {
		t.Fatalf("authorized: %v", err)
	}
	if got := rig.status(t, trip.ID).Status; got != domain.StatusRequested {
		t.Fatalf("after authorization %s, want requested", got)
	}
	if len(rig.matcher.trips) != 1 {
		t.Fatalf("the matcher was asked %d times, want once", len(rig.matcher.trips))
	}

	// Once a driver has it, a late authorization changes nothing and asks for
	// nobody else.
	if _, err := rig.service.Matched(ctx, trip.ID, "drv-1"); err != nil {
		t.Fatalf("matched: %v", err)
	}
	if err := rig.service.PaymentAuthorized(ctx, trip.ID); err != nil {
		t.Fatalf("a late authorization: %v", err)
	}
	if len(rig.matcher.trips) != 1 || rig.status(t, trip.ID).Status != domain.StatusAccepted {
		t.Errorf("a late authorization disturbed an accepted trip")
	}
}

func TestAFailedPaymentCancelsTheBooking(t *testing.T) {
	rig := gatedRig(t)
	ctx := context.Background()
	trip := mustCreate(t, rig, "key-declined")

	for range 2 { // the second is a redelivery
		if err := rig.service.PaymentFailed(ctx, trip.ID); err != nil {
			t.Fatalf("failed: %v", err)
		}
	}

	stored := rig.status(t, trip.ID)
	if stored.Status != domain.StatusCancelled || stored.CancelReason != service.CancelReasonPaymentFailed {
		t.Errorf("stored %s (%q), want cancelled for payment_failed", stored.Status, stored.CancelReason)
	}
	if len(rig.matcher.trips) != 0 {
		t.Error("a trip that could not be paid for was dispatched")
	}
	assertKinds(t, rig.trips.Facts(), domain.FactRequested, domain.FactCancelled)
}

// A payment failure never pulls a trip out from under a driver on their way.
func TestAPaymentFailureCannotCancelADispatchedTrip(t *testing.T) {
	rig := gatedRig(t)
	ctx := context.Background()
	trip := mustCreate(t, rig, "key-dispatched")

	if err := rig.service.PaymentAuthorized(ctx, trip.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.service.Matched(ctx, trip.ID, "drv-1"); err != nil {
		t.Fatal(err)
	}
	if err := rig.service.PaymentFailed(ctx, trip.ID); err != nil {
		t.Fatalf("failed: %v", err)
	}
	if got := rig.status(t, trip.ID).Status; got != domain.StatusAccepted {
		t.Errorf("a payment failure moved an accepted trip to %s", got)
	}
}

// A rider waiting on a slow card can still change their mind.
func TestTheRiderCanCancelWhileTheFareIsHeld(t *testing.T) {
	rig := gatedRig(t)
	trip := mustCreate(t, rig, "key-impatient")

	cancelled, err := rig.service.Cancel(context.Background(), trip.ID, "rider")
	if err != nil || cancelled.Status != domain.StatusCancelled {
		t.Fatalf("cancel while payment_pending: %v, %+v", err, cancelled)
	}
}
