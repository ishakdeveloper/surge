package repository_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
	"github.com/ishakdeveloper/surge/services/payments/internal/infrastructure/repository"
	"github.com/ishakdeveloper/surge/shared/pgtest"
)

// Refunds are added in place and bounded by what was captured, so refunds in
// parts cannot together give back more than was taken.
func TestRefundsAreBoundedByTheCapture(t *testing.T) {
	pool := pgtest.Pool(t)
	repo := repository.NewPostgres(pool)
	ctx := context.Background()

	payment := &domain.Payment{ID: "pay-1", TripID: "trip-1", Attempt: 1, RiderID: "rider-1", DriverID: "drv-1",
		Status: domain.StatusCaptured, AmountCents: 1450, CapturedCents: 1450, Currency: "eur",
		ProcessorPaymentID: "pi_1", CreatedAt: at, UpdatedAt: at}
	earning := domain.NewEarning(payment, 2000, at)
	if err := repo.Apply(ctx, domain.Change{Payment: payment, Earning: &earning}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	refund := func(id, key string, amount, driver int64) error {
		return repo.Apply(ctx, domain.Change{Refund: &domain.Refund{
			ID: id, TripID: "trip-1", PaymentID: "pay-1", AmountCents: amount, DriverCents: driver,
			Currency: "eur", Reversal: domain.ReversalDeducted, IdempotencyKey: key, CreatedAt: at,
		}})
	}

	if err := refund("ref-1", "k1", 1000, 800); err != nil {
		t.Fatalf("first refund: %v", err)
	}
	if err := refund("ref-2", "k1", 100, 80); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("a second refund under the same key: want ErrConflict, got %v", err)
	}
	if err := refund("ref-3", "k2", 500, 400); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("500 more of the 450 left: want ErrConflict, got %v", err)
	}
	if err := refund("ref-4", "k3", 450, 360); err != nil {
		t.Fatalf("the rest: %v", err)
	}

	stored, _ := repo.PaymentForTrip(ctx, "trip-1")
	if stored.Status != domain.StatusRefunded || stored.RefundedCents != 1450 {
		t.Errorf("payment %s refunded %d, want refunded in full", stored.Status, stored.RefundedCents)
	}
	back, _ := repo.Earning(ctx, "trip-1")
	if back.ReversedCents != 1160 || back.Status != domain.EarningReversed || back.Payable() != 0 {
		t.Errorf("earning %+v, want all of it reversed", back)
	}
	if found, err := repo.RefundByKey(ctx, "trip-1", "k1"); err != nil || found.ID != "ref-1" {
		t.Errorf("refund by key: %+v (%v)", found, err)
	}
}

func TestWithdrawalsAreKeyedAndCompareAndSet(t *testing.T) {
	pool := pgtest.Pool(t)
	repo := repository.NewPostgres(pool)
	ctx := context.Background()

	withdrawal := &domain.Withdrawal{ID: "wd-1", DriverID: "drv-1", AmountCents: 1160, Currency: "eur",
		Status: domain.WithdrawalRequested, IdempotencyKey: "tap-1", CreatedAt: at, UpdatedAt: at}
	if err := repo.SaveWithdrawal(ctx, withdrawal); err != nil {
		t.Fatalf("save: %v", err)
	}
	double := *withdrawal
	double.ID = "wd-2"
	if err := repo.SaveWithdrawal(ctx, &double); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("the double tap: want ErrConflict, got %v", err)
	}

	sent := *withdrawal
	sent.Status, sent.ProcessorPayoutID = domain.WithdrawalInTransit, "po_1"
	if err := repo.UpdateWithdrawal(ctx, &sent, domain.WithdrawalRequested); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := repo.UpdateWithdrawal(ctx, &sent, domain.WithdrawalRequested); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("a stale update: want ErrConflict, got %v", err)
	}

	byPayout, err := repo.WithdrawalByPayoutID(ctx, "po_1")
	if err != nil || byPayout.ID != "wd-1" {
		t.Errorf("by payout: %+v (%v)", byPayout, err)
	}
	page, err := repo.ListWithdrawals(ctx, domain.WithdrawalFilter{DriverID: "drv-1", Limit: 10})
	if err != nil || len(page.Withdrawals) != 1 || page.Withdrawals[0].Status != domain.WithdrawalInTransit {
		t.Errorf("list: %+v (%v)", page, err)
	}
}
