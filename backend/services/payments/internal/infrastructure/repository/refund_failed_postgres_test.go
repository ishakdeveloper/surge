package repository_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
	"github.com/ishakdeveloper/surge/services/payments/internal/infrastructure/repository"
	"github.com/ishakdeveloper/surge/shared/pgtest"
)

// A failed refund gives its amount back to the payment once, however often the
// failure is delivered, and a payment refunded in full is captured again.
func TestAFailedRefundGivesItsAmountBack(t *testing.T) {
	pool := pgtest.Pool(t)
	repo := repository.NewPostgres(pool)
	ctx := context.Background()

	payment := &domain.Payment{ID: "pay-1", TripID: "trip-1", Attempt: 1, RiderID: "rider-1",
		Status: domain.StatusCaptured, AmountCents: 1450, CapturedCents: 1450, Currency: "eur",
		ProcessorPaymentID: "pi_1", CreatedAt: at, UpdatedAt: at}
	if err := repo.Apply(ctx, domain.Change{Payment: payment}); err != nil {
		t.Fatal(err)
	}
	refund := &domain.Refund{ID: "ref-1", TripID: "trip-1", PaymentID: "pay-1", AmountCents: 1450,
		Currency: "eur", Reversal: domain.ReversalNone, Status: domain.RefundSucceeded,
		ProcessorRefundID: "re_1", IdempotencyKey: "k1", CreatedAt: at}
	if err := repo.Apply(ctx, domain.Change{Refund: refund}); err != nil {
		t.Fatal(err)
	}

	failed := *refund
	failed.Status, failed.FailureReason = domain.RefundFailed, "expired_or_canceled_card"
	if err := repo.Apply(ctx, domain.Change{FailedRefund: &failed}); err != nil {
		t.Fatalf("fail: %v", err)
	}
	if err := repo.Apply(ctx, domain.Change{FailedRefund: &failed}); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("failing twice: want ErrConflict, got %v", err)
	}

	stored, _ := repo.PaymentForTrip(ctx, "trip-1")
	if stored.Status != domain.StatusCaptured || stored.RefundedCents != 0 {
		t.Errorf("payment %s refunded %d, want captured with nothing refunded", stored.Status, stored.RefundedCents)
	}
	found, err := repo.RefundByProcessorID(ctx, "re_1")
	if err != nil || found.Status != domain.RefundFailed || found.FailureReason != "expired_or_canceled_card" {
		t.Errorf("refund by processor id: %+v (%v)", found, err)
	}
}
