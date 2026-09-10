package service_test

import (
	"context"
	"testing"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
	"github.com/ishakdeveloper/surge/services/payments/internal/infrastructure/fake"
)

// A refund that never reached the card is undone: the payment can be refunded
// again, the driver's reversed share goes back to them, and the ledger ends
// where it would have with no refund at all.
func TestAFailedRefundAfterPayoutIsUndone(t *testing.T) {
	rig := newRig(t)
	ctx := context.Background()
	rig.card(t, "rider-1", fake.CardVisa)
	rig.account(t, "drv-1", domain.TransfersActive)
	rig.completed(t, "trip-1")

	refund, err := rig.service.Refund(ctx, "trip-1", 0, "requested_by_customer", "ops-1")
	if err != nil || refund.Reversal != domain.ReversalReversed {
		t.Fatalf("refund %+v (%v)", refund, err)
	}

	rig.processor.FailRefund(refund.ProcessorRefundID)
	for range 2 { // delivered twice
		if err := rig.service.RefundFailed(ctx, refund.ProcessorRefundID, "expired_or_canceled_card"); err != nil {
			t.Fatalf("refund failed: %v", err)
		}
	}

	payment := rig.payment(t, "trip-1")
	if payment.Status != domain.StatusCaptured || payment.RefundedCents != 0 {
		t.Errorf("payment %s refunded %d, want captured and refundable again", payment.Status, payment.RefundedCents)
	}
	stored, _ := rig.repo.RefundByKey(ctx, "trip-1", "ops-1")
	if stored.Status != domain.RefundFailed || stored.FailureReason != "expired_or_canceled_card" || stored.ProcessorRestoreTransferID == "" {
		t.Errorf("refund %+v, want failed with its reason and a restore transfer", stored)
	}
	if calls := rig.processor.Calls(); calls.Transfer != 2 {
		t.Errorf("transferred %d times, want the original and one restore", calls.Transfer)
	}
	if earning, _ := rig.repo.Earning(ctx, "trip-1"); earning.Payable() != 1160 || earning.Status != domain.EarningTransferred {
		t.Errorf("earning %+v, want all 1160 the driver's again", earning)
	}

	for account, want := range map[string]int64{
		domain.AccountClearing: 290, domain.AccountRevenue: -290, domain.DriverAccount("drv-1"): 0,
	} {
		if got := rig.balance(t, account); got != want {
			t.Errorf("%s holds %d, want %d as if there had been no refund", account, got, want)
		}
	}

	// Refundable again: ops can repay the rider another way.
	if _, err := rig.service.Refund(ctx, "trip-1", 0, "second attempt", "ops-2"); err != nil {
		t.Errorf("refunding again after a failure: %v", err)
	}
}

// Deducted while unpaid, then paid out with the rest before the refund failed:
// giving the share back means sending it, not owing it.
func TestAFailedRefundDeductedThenPaidOutIsSent(t *testing.T) {
	rig := newRig(t)
	ctx := context.Background()
	rig.card(t, "rider-1", fake.CardVisa)
	rig.account(t, "drv-1", "pending")
	rig.completed(t, "trip-1")

	refund, err := rig.service.Refund(ctx, "trip-1", 725, "late pickup", "ops-1")
	if err != nil || refund.Reversal != domain.ReversalDeducted {
		t.Fatalf("refund %+v (%v)", refund, err)
	}
	rig.account(t, "drv-1", domain.TransfersActive)
	if err := rig.service.PayOutstanding(ctx, "drv-1"); err != nil {
		t.Fatal(err)
	}

	rig.processor.FailRefund(refund.ProcessorRefundID)
	if err := rig.service.RefundFailed(ctx, refund.ProcessorRefundID, "bank_refused"); err != nil {
		t.Fatalf("refund failed: %v", err)
	}
	if calls := rig.processor.Calls(); calls.Transfer != 2 {
		t.Errorf("transferred %d times, want the 580 payable and the 580 given back", calls.Transfer)
	}
	if owed := rig.balance(t, domain.DriverAccount("drv-1")); owed != 0 {
		t.Errorf("driver account %d, want 0", owed)
	}
}

// An unknown refund is acknowledged, not retried: it is another integration's.
func TestAFailedRefundThisServiceNeverMade(t *testing.T) {
	rig := newRig(t)
	if err := rig.service.RefundFailed(context.Background(), "re_elsewhere", "x"); err == nil {
		t.Error("an unknown refund should be reported as not found, for the webhook to acknowledge")
	}
}
