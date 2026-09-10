package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
	"github.com/ishakdeveloper/surge/services/payments/internal/infrastructure/repository"
	"github.com/ishakdeveloper/surge/shared/pgtest"
)

func TestDisputesAreOnePerProcessorDispute(t *testing.T) {
	pool := pgtest.Pool(t)
	repo := repository.NewPostgres(pool)
	ctx := context.Background()

	dispute := &domain.Dispute{ID: "dsp-1", ProcessorDisputeID: "dp_1", TripID: "trip-1", PaymentID: "pay-1",
		AmountCents: 1450, Currency: "eur", Reversal: domain.ReversalNone, Status: domain.DisputeOpen,
		CreatedAt: at, UpdatedAt: at}
	if err := repo.Apply(ctx, domain.Change{Dispute: dispute}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	again := *dispute
	again.ID = "dsp-2"
	if err := repo.Apply(ctx, domain.Change{Dispute: &again}); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("the same processor dispute twice: want ErrConflict, got %v", err)
	}

	won := *dispute
	won.Status = domain.DisputeWon
	if err := repo.Apply(ctx, domain.Change{Dispute: &won, DisputeFrom: domain.DisputeOpen}); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := repo.Apply(ctx, domain.Change{Dispute: &won, DisputeFrom: domain.DisputeOpen}); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("closing twice: want ErrConflict, got %v", err)
	}
	if stored, err := repo.DisputeByProcessorID(ctx, "dp_1"); err != nil || stored.Status != domain.DisputeWon {
		t.Errorf("stored %+v (%v)", stored, err)
	}
}

func TestTheSweepersQueries(t *testing.T) {
	pool := pgtest.Pool(t)
	repo := repository.NewPostgres(pool)
	ctx := context.Background()

	for i, updated := range []time.Time{at, at.Add(time.Hour)} {
		payment := &domain.Payment{ID: string(rune('a' + i)), TripID: string(rune('a'+i)) + "-trip", Attempt: 1,
			RiderID: "rider-1", Status: domain.StatusRequiresAction, AmountCents: 1450, Currency: "eur",
			CreatedAt: updated, UpdatedAt: updated}
		if err := repo.Apply(ctx, domain.Change{Payment: payment}); err != nil {
			t.Fatal(err)
		}
	}
	stale, err := repo.StalePayments(ctx, domain.StatusRequiresAction, at.Add(30*time.Minute), 10)
	if err != nil || len(stale) != 1 || stale[0].ID != "a" {
		t.Errorf("stale %+v (%v), want only the older one", stale, err)
	}

	if err := repo.SavePayoutAccount(ctx, domain.PayoutAccount{DriverID: "drv-1", ProcessorAccountID: "acct_1",
		TransfersStatus: domain.TransfersActive, CreatedAt: at, UpdatedAt: at}); err != nil {
		t.Fatal(err)
	}
	earning := domain.Earning{TripID: "a-trip", DriverID: "drv-1", PaymentID: "a", GrossCents: 1450,
		NetCents: 1160, Currency: "eur", Status: domain.EarningUnpaid, CreatedAt: at, UpdatedAt: at}
	if err := repo.Apply(ctx, domain.Change{Earning: &earning}); err != nil {
		t.Fatal(err)
	}
	if drivers, err := repo.OwedDrivers(ctx, 10); err != nil || len(drivers) != 1 || drivers[0] != "drv-1" {
		t.Errorf("owed drivers %v (%v)", drivers, err)
	}
}
