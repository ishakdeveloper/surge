package repository_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
	"github.com/ishakdeveloper/surge/services/payments/internal/infrastructure/repository"
	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/ishakdeveloper/surge/shared/pgtest"
	"github.com/ishakdeveloper/surge/shared/wire"
	"github.com/jackc/pgx/v5/pgxpool"
)

var at = time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

func published(t *testing.T, pool *pgxpool.Pool) []wire.PaymentFact {
	t.Helper()
	rows, err := pool.Query(context.Background(), `select topic, key, value from payments_outbox order by id`)
	if err != nil {
		t.Fatalf("read outbox: %v", err)
	}
	defer rows.Close()

	var facts []wire.PaymentFact
	for rows.Next() {
		var (
			topic, key string
			value      []byte
			fact       wire.PaymentFact
		)
		if err := rows.Scan(&topic, &key, &value); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if err := json.Unmarshal(value, &fact); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if topic != kafkax.TopicPaymentEvents || key != fact.TripID {
			t.Errorf("fact for %s written to %s keyed %q", fact.TripID, topic, key)
		}
		facts = append(facts, fact)
	}
	return facts
}

// The capture, the earning, the ledger and the fact commit together — and a
// capture applied twice commits none of them the second time.
func TestACaptureIsOneWrite(t *testing.T) {
	pool := pgtest.Pool(t)
	repo := repository.NewPostgres(pool)
	ctx := context.Background()

	payment := &domain.Payment{ID: "pay-1", TripID: "trip-1", Attempt: 1, RiderID: "rider-1",
		Status: domain.StatusAuthorized, AmountCents: 1450, Currency: "eur",
		ProcessorPaymentID: "pi_1", CreatedAt: at, UpdatedAt: at}
	if err := repo.Apply(ctx, domain.Change{Payment: payment}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	// One payment per trip.
	duplicate := *payment
	duplicate.ID = "pay-2"
	if err := repo.Apply(ctx, domain.Change{Payment: &duplicate}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("a second payment for one trip: want ErrConflict, got %v", err)
	}

	captured := *payment
	captured.DriverID = "drv-1"
	captured.CapturedCents = 1450
	captured.ProcessorChargeID = "ch_1"
	if err := captured.Transition(domain.StatusCaptured, at); err != nil {
		t.Fatal(err)
	}
	earning := domain.NewEarning(&captured, 2000, at)
	captured.CommissionCents = earning.CommissionCents
	change := domain.Change{
		Payment: &captured, From: domain.StatusAuthorized,
		Earning: &earning,
		Txns:    []domain.Txn{domain.CaptureTxn(&captured, earning)},
		Facts:   domain.FactsOf(&captured, domain.StatusAuthorized),
	}
	if err := repo.Apply(ctx, change); err != nil {
		t.Fatalf("capture: %v", err)
	}
	if err := repo.Apply(ctx, change); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("the same capture again: want ErrConflict, got %v", err)
	}

	stored, err := repo.PaymentByProcessorID(ctx, "pi_1")
	if err != nil || stored.Status != domain.StatusCaptured || stored.CommissionCents != 290 {
		t.Fatalf("stored %+v (%v)", stored, err)
	}
	owed, _ := repo.LedgerBalance(ctx, domain.DriverAccount("drv-1"))
	clearing, _ := repo.LedgerBalance(ctx, domain.AccountClearing)
	if owed != -1160 || clearing != 1450 {
		t.Errorf("the ledger owes the driver %d and holds %d, want -1160 and 1450", owed, clearing)
	}
	if facts := published(t, pool); len(facts) != 1 || facts[0].Tag != wire.FactPaymentCaptured {
		t.Errorf("outbox holds %+v, want one capture", facts)
	}

	unpaid, err := repo.UnpaidEarnings(ctx, "drv-1")
	if err != nil || len(unpaid) != 1 || unpaid[0].NetCents != 1160 {
		t.Fatalf("unpaid earnings %+v (%v)", unpaid, err)
	}

	// Paying it out is a compare-and-set on the earning.
	paid := unpaid[0]
	paid.Status = domain.EarningTransferred
	paid.ProcessorTransferID = "tr_1"
	transfer := domain.Change{Earning: &paid, EarningFrom: domain.EarningUnpaid, Txns: []domain.Txn{domain.TransferTxn(paid)}}
	if err := repo.Apply(ctx, transfer); err != nil {
		t.Fatalf("transfer: %v", err)
	}
	if err := repo.Apply(ctx, transfer); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("the same transfer again: want ErrConflict, got %v", err)
	}
	if owed, _ := repo.LedgerBalance(ctx, domain.DriverAccount("drv-1")); owed != 0 {
		t.Errorf("the driver is still owed %d after being paid", owed)
	}
}

func TestCustomersAndAccountsAreUpserts(t *testing.T) {
	pool := pgtest.Pool(t)
	repo := repository.NewPostgres(pool)
	ctx := context.Background()

	if _, err := repo.Customer(ctx, "rider-1"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("an unknown rider: want ErrNotFound, got %v", err)
	}

	customer := domain.Customer{UserID: "rider-1", ProcessorCustomerID: "cus_1", CreatedAt: at, UpdatedAt: at}
	if err := repo.SaveCustomer(ctx, customer); err != nil {
		t.Fatalf("save: %v", err)
	}
	customer.PaymentMethodID = "pm_card_visa"
	customer.Card = domain.Card{Brand: "visa", Last4: "4242", ExpMonth: 12, ExpYear: 2030}
	if err := repo.SaveCustomer(ctx, customer); err != nil {
		t.Fatalf("save again: %v", err)
	}
	stored, err := repo.Customer(ctx, "rider-1")
	if err != nil || !stored.CanPay() || stored.Card.Last4 != "4242" {
		t.Errorf("stored %+v (%v)", stored, err)
	}

	account := domain.PayoutAccount{DriverID: "drv-1", ProcessorAccountID: "acct_1",
		TransfersStatus: "pending", RequirementsDue: true, CreatedAt: at, UpdatedAt: at}
	if err := repo.SavePayoutAccount(ctx, account); err != nil {
		t.Fatalf("save account: %v", err)
	}
	account.TransfersStatus, account.RequirementsDue = domain.TransfersActive, false
	if err := repo.SavePayoutAccount(ctx, account); err != nil {
		t.Fatalf("save account again: %v", err)
	}
	if stored, err := repo.PayoutAccount(ctx, "drv-1"); err != nil || !stored.CanReceive() {
		t.Errorf("stored %+v (%v)", stored, err)
	}
}
