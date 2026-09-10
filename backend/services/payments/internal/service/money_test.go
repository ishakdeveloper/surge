package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
	"github.com/ishakdeveloper/surge/services/payments/internal/infrastructure/fake"
	"github.com/ishakdeveloper/surge/services/payments/internal/service"
)

// completed takes trip-1 from request to capture, with the driver's account in
// whatever state the test set up.
func (r *rig) completed(t *testing.T, tripID string) {
	t.Helper()
	ctx := context.Background()
	if err := r.service.TripRequested(ctx, trip(tripID)); err != nil {
		t.Fatal(err)
	}
	if err := r.service.TripCompleted(ctx, trip(tripID)); err != nil {
		t.Fatal(err)
	}
}

// The driver takes out what their account holds, once however often they tap.
func TestAWithdrawalPaysOutWhatIsAvailable(t *testing.T) {
	rig := newRig(t)
	ctx := context.Background()
	rig.card(t, "rider-1", fake.CardVisa)
	if _, err := rig.service.StartOnboarding(ctx, "drv-1", "driver@example.com"); err != nil {
		t.Fatal(err)
	}
	rig.completed(t, "trip-1")

	balance, err := rig.service.Balance(ctx, "drv-1")
	if err != nil || balance.AvailableCents != 1160 || !balance.CanWithdraw {
		t.Fatalf("balance %+v (%v), want 1160 available", balance, err)
	}

	var first *domain.Withdrawal
	for range 2 { // the double tap
		withdrawal, err := rig.service.Withdraw(ctx, "drv-1", 0, "tap-1")
		if err != nil {
			t.Fatalf("withdraw: %v", err)
		}
		if first == nil {
			first = withdrawal
		}
		if withdrawal.ID != first.ID {
			t.Fatal("a repeated tap made a second withdrawal")
		}
	}
	if first.Status != domain.WithdrawalInTransit || first.AmountCents != 1160 {
		t.Errorf("withdrawal %+v, want 1160 in transit", first)
	}
	if calls := rig.processor.Calls(); calls.Payout != 1 {
		t.Errorf("paid out %d times, want once", calls.Payout)
	}

	if err := rig.service.PayoutSettled(ctx, first.ProcessorPayoutID, "paid", ""); err != nil {
		t.Fatalf("settled: %v", err)
	}
	page, _ := rig.service.Withdrawals(ctx, "drv-1", 0, "")
	if len(page.Withdrawals) != 1 || page.Withdrawals[0].Status != domain.WithdrawalPaid {
		t.Errorf("withdrawals %+v, want one paid", page.Withdrawals)
	}

	if _, err := rig.service.Withdraw(ctx, "drv-1", 0, "tap-2"); !errors.Is(err, service.ErrNothingToWithdraw) {
		t.Errorf("withdrawing from an empty balance: want ErrNothingToWithdraw, got %v", err)
	}
}

func TestAWithdrawalIsBoundedByTheBalance(t *testing.T) {
	rig := newRig(t)
	ctx := context.Background()
	rig.card(t, "rider-1", fake.CardVisa)

	if _, err := rig.service.Withdraw(ctx, "drv-1", 100, "tap-1"); !errors.Is(err, service.ErrNoPayoutAccount) {
		t.Errorf("without an account: want ErrNoPayoutAccount, got %v", err)
	}

	if _, err := rig.service.StartOnboarding(ctx, "drv-1", "driver@example.com"); err != nil {
		t.Fatal(err)
	}
	rig.completed(t, "trip-1")
	if _, err := rig.service.Withdraw(ctx, "drv-1", 5000, "tap-2"); !errors.Is(err, service.ErrOverBalance) {
		t.Errorf("more than the balance: want ErrOverBalance, got %v", err)
	}
	if _, err := rig.service.Withdraw(ctx, "drv-1", 100, ""); !errors.Is(err, service.ErrIdempotencyKeyRequired) {
		t.Errorf("no key: want ErrIdempotencyKeyRequired, got %v", err)
	}
}

// Refunded before the driver was paid: nothing to fetch back, only less to
// pay, and nothing left over anywhere in the ledger.
func TestAFullRefundBeforePayout(t *testing.T) {
	rig := newRig(t)
	ctx := context.Background()
	rig.card(t, "rider-1", fake.CardVisa)
	rig.account(t, "drv-1", "pending")
	rig.completed(t, "trip-1")

	refund, err := rig.service.Refund(ctx, "trip-1", 0, "requested_by_customer", "ops-1")
	if err != nil {
		t.Fatalf("refund: %v", err)
	}
	if refund.AmountCents != 1450 || refund.DriverCents != 1160 || refund.Reversal != domain.ReversalDeducted {
		t.Errorf("refund %+v, want 1450 with 1160 deducted", refund)
	}
	if calls := rig.processor.Calls(); calls.Reverse != 0 {
		t.Error("reversed a transfer that was never made")
	}
	if payment := rig.payment(t, "trip-1"); payment.Status != domain.StatusRefunded || payment.RefundedCents != 1450 {
		t.Errorf("payment %s refunded %d", payment.Status, payment.RefundedCents)
	}

	for _, account := range []string{domain.AccountClearing, domain.AccountRevenue, domain.DriverAccount("drv-1")} {
		if balance := rig.balance(t, account); balance != 0 {
			t.Errorf("%s holds %d after a full refund, want 0", account, balance)
		}
	}

	// The driver's account activating now has nothing to pay them.
	rig.account(t, "drv-1", domain.TransfersActive)
	if err := rig.service.PayOutstanding(ctx, "drv-1"); err != nil {
		t.Fatal(err)
	}
	if calls := rig.processor.Calls(); calls.Transfer != 0 {
		t.Error("paid a driver for a trip refunded in full")
	}
}

// Refunded after the driver was paid: their share comes back from their
// balance, in proportion.
func TestAPartialRefundAfterPayoutReversesTheDriversShare(t *testing.T) {
	rig := newRig(t)
	ctx := context.Background()
	rig.card(t, "rider-1", fake.CardVisa)
	rig.account(t, "drv-1", domain.TransfersActive)
	rig.completed(t, "trip-1")

	refund, err := rig.service.Refund(ctx, "trip-1", 725, "late pickup", "ops-1")
	if err != nil {
		t.Fatalf("refund: %v", err)
	}
	if refund.DriverCents != 580 || refund.Reversal != domain.ReversalReversed || refund.ProcessorReversalID == "" {
		t.Errorf("refund %+v, want 580 reversed", refund)
	}
	if payment := rig.payment(t, "trip-1"); payment.Status != domain.StatusCaptured || payment.RefundedCents != 725 {
		t.Errorf("payment %s refunded %d, want still captured with 725 back", payment.Status, payment.RefundedCents)
	}

	// €7.25 back to the rider: €5.80 of it from the driver, €1.45 from the
	// platform's commission, and the driver owes nothing.
	if owed := rig.balance(t, domain.DriverAccount("drv-1")); owed != 0 {
		t.Errorf("driver account %d, want 0 after the reversal", owed)
	}
	if revenue := rig.balance(t, domain.AccountRevenue); revenue != -145 {
		t.Errorf("revenue %d, want -145", revenue)
	}

	// The same click again is the same refund; more than is left is refused.
	again, err := rig.service.Refund(ctx, "trip-1", 725, "late pickup", "ops-1")
	if err != nil || again.ID != refund.ID {
		t.Errorf("a retried refund: %+v (%v), want the first", again, err)
	}
	if _, err := rig.service.Refund(ctx, "trip-1", 1000, "again", "ops-2"); !errors.Is(err, service.ErrRefundTooLarge) {
		t.Errorf("more than is left: want ErrRefundTooLarge, got %v", err)
	}
}

// The driver has already withdrawn their share: the rider is refunded anyway,
// the platform carries it, and the ledger says the driver owes it.
func TestARefundTheDriverCannotCoverIsCarriedByThePlatform(t *testing.T) {
	rig := newRig(t)
	ctx := context.Background()
	rig.card(t, "rider-1", fake.CardVisa)
	rig.account(t, "drv-1", domain.TransfersActive)
	rig.completed(t, "trip-1")
	rig.processor.Drain("acct_drv-1")

	refund, err := rig.service.Refund(ctx, "trip-1", 0, "fraudulent", "ops-1")
	if err != nil {
		t.Fatalf("refund: %v", err)
	}
	if refund.Reversal != domain.ReversalFailed {
		t.Errorf("reversal %s, want failed", refund.Reversal)
	}
	if owed := rig.balance(t, domain.DriverAccount("drv-1")); owed != 1160 {
		t.Errorf("driver account %d, want 1160 owed to the platform", owed)
	}
}

func TestOnlyACaptureCanBeRefunded(t *testing.T) {
	rig := newRig(t)
	ctx := context.Background()
	rig.card(t, "rider-1", fake.CardVisa)
	if err := rig.service.TripRequested(ctx, trip("trip-1")); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.service.Refund(ctx, "trip-1", 0, "", "ops-1"); !errors.Is(err, service.ErrNotRefundable) {
		t.Errorf("refunding a hold: want ErrNotRefundable, got %v", err)
	}
}
