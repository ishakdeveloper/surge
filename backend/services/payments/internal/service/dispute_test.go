package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
	"github.com/ishakdeveloper/surge/services/payments/internal/infrastructure/fake"
	"github.com/ishakdeveloper/surge/services/payments/internal/service"
)

func (r *rig) disputed(t *testing.T, tripID, disputeID string) {
	t.Helper()
	payment := r.payment(t, tripID)
	opened := service.DisputeOpened{
		ProcessorDisputeID: disputeID, ProcessorPaymentID: payment.ProcessorPaymentID,
		AmountCents: payment.CapturedCents, Reason: "fraudulent",
	}
	for range 2 { // delivered twice
		if err := r.service.DisputeOpened(context.Background(), opened); err != nil {
			t.Fatalf("dispute opened: %v", err)
		}
	}
}

// Paid out, disputed and won: the driver's share comes back from their balance
// while the dispute is open, and goes to them again once it is won. The ledger
// ends where it would have if nothing had happened.
func TestADisputeWonAfterPayoutGivesTheShareBack(t *testing.T) {
	rig := newRig(t)
	ctx := context.Background()
	rig.card(t, "rider-1", fake.CardVisa)
	rig.account(t, "drv-1", domain.TransfersActive)
	rig.completed(t, "trip-1")

	rig.disputed(t, "trip-1", "dp_1")
	if calls := rig.processor.Calls(); calls.Reverse != 1 {
		t.Fatalf("reversed %d times for one dispute delivered twice, want once", calls.Reverse)
	}
	if earning, _ := rig.repo.Earning(ctx, "trip-1"); earning.Payable() != 0 || earning.Status != domain.EarningReversed {
		t.Fatalf("earning while disputed: %+v", earning)
	}

	for range 2 {
		if err := rig.service.DisputeClosed(ctx, "dp_1", true); err != nil {
			t.Fatalf("closed: %v", err)
		}
	}
	if calls := rig.processor.Calls(); calls.Transfer != 2 {
		t.Errorf("transferred %d times, want the original and one restore", calls.Transfer)
	}
	if earning, _ := rig.repo.Earning(ctx, "trip-1"); earning.Payable() != 1160 || earning.Status != domain.EarningTransferred {
		t.Errorf("earning after winning: %+v", earning)
	}

	for account, want := range map[string]int64{
		domain.AccountClearing: 290, domain.AccountRevenue: -290, domain.DriverAccount("drv-1"): 0,
	} {
		if got := rig.balance(t, account); got != want {
			t.Errorf("%s holds %d, want %d as if nothing had happened", account, got, want)
		}
	}
}

// Disputed before payout and lost: the driver is owed nothing for the trip.
func TestADisputeLostBeforePayoutLeavesNothingOwed(t *testing.T) {
	rig := newRig(t)
	ctx := context.Background()
	rig.card(t, "rider-1", fake.CardVisa)
	rig.account(t, "drv-1", "pending")
	rig.completed(t, "trip-1")

	rig.disputed(t, "trip-1", "dp_1")
	if err := rig.service.DisputeClosed(ctx, "dp_1", false); err != nil {
		t.Fatalf("closed: %v", err)
	}

	_, totals, _ := rig.service.Earnings(ctx, "drv-1", 0, "")
	if totals.OwedCents != 0 {
		t.Errorf("owed %d after a lost dispute, want 0", totals.OwedCents)
	}
	if owed := rig.balance(t, domain.DriverAccount("drv-1")); owed != 0 {
		t.Errorf("driver account %d, want 0", owed)
	}
}

// Disputed before payout and won: owed again, and paid when they can be.
func TestADisputeWonBeforePayoutIsOwedAgain(t *testing.T) {
	rig := newRig(t)
	ctx := context.Background()
	rig.card(t, "rider-1", fake.CardVisa)
	rig.account(t, "drv-1", "pending")
	rig.completed(t, "trip-1")

	rig.disputed(t, "trip-1", "dp_1")
	if err := rig.service.DisputeClosed(ctx, "dp_1", true); err != nil {
		t.Fatalf("closed: %v", err)
	}
	_, totals, _ := rig.service.Earnings(ctx, "drv-1", 0, "")
	if totals.OwedCents != 1160 {
		t.Errorf("owed %d after winning, want 1160", totals.OwedCents)
	}
}

// Already withdrawn: the reversal fails, the platform carries it, and winning
// clears what the ledger said the driver owed.
func TestADisputeTheDriverCannotCover(t *testing.T) {
	rig := newRig(t)
	ctx := context.Background()
	rig.card(t, "rider-1", fake.CardVisa)
	rig.account(t, "drv-1", domain.TransfersActive)
	rig.completed(t, "trip-1")
	rig.processor.Drain("acct_drv-1")

	rig.disputed(t, "trip-1", "dp_1")
	if owed := rig.balance(t, domain.DriverAccount("drv-1")); owed != 1160 {
		t.Fatalf("driver account %d while disputed, want 1160 owed to the platform", owed)
	}
	if err := rig.service.DisputeClosed(ctx, "dp_1", true); err != nil {
		t.Fatalf("closed: %v", err)
	}
	if owed := rig.balance(t, domain.DriverAccount("drv-1")); owed != 0 {
		t.Errorf("driver account %d after winning, want 0", owed)
	}
}

// The sweeper: a rider who never authenticates has the hold let go and the trip
// failed; a hold whose trip never ends is released; fresh ones are left alone.
func TestTheSweeperExpiresAndReleases(t *testing.T) {
	rig := newRig(t)
	ctx := context.Background()
	rig.card(t, "rider-1", fake.CardAuthenticationRequired)
	if err := rig.service.TripRequested(ctx, trip("trip-3ds")); err != nil {
		t.Fatal(err)
	}
	rig.card(t, "rider-1", fake.CardVisa)
	if err := rig.service.TripRequested(ctx, trip("trip-held")); err != nil {
		t.Fatal(err)
	}

	// Minutes later: nothing is stale yet.
	result, err := rig.sweepAt(t, now.Add(time.Minute))
	if err != nil || result != (service.SweepResult{}) {
		t.Fatalf("an early sweep did %+v (%v)", result, err)
	}

	result, err = rig.sweepAt(t, now.Add(25*time.Hour))
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if result.Expired != 1 || result.Released != 1 {
		t.Errorf("swept %+v, want one expired and one released", result)
	}

	expired := rig.payment(t, "trip-3ds")
	if expired.Status != domain.StatusFailed || expired.FailureReason != domain.FailureExpired {
		t.Errorf("the 3-D Secure hold is %s (%q), want failed as expired", expired.Status, expired.FailureReason)
	}
	if held := rig.payment(t, "trip-held"); held.Status != domain.StatusReleased {
		t.Errorf("the stale hold is %s, want released", held.Status)
	}
	if calls := rig.processor.Calls(); calls.Release != 2 {
		t.Errorf("released %d holds at the processor, want 2", calls.Release)
	}

	// The expiry is a failure fact, which is what cancels the waiting trip.
	facts := rig.repo.Facts()
	if last := facts[len(facts)-1]; last.Kind != domain.FactFailed || last.Payment.TripID != "trip-3ds" {
		t.Errorf("last fact %s for %s, want a failure for trip-3ds", last.Kind, last.Payment.TripID)
	}
}
