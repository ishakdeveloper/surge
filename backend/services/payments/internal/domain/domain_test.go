package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
)

// A driver is never paid a cent that was not charged, whatever the rounding.
func TestSplitNeverOverpaysTheDriver(t *testing.T) {
	cases := []struct {
		gross, commission, net int64
		bps                    int
	}{
		{gross: 1450, bps: 2000, commission: 290, net: 1160},
		// 800.8 for the driver: rounded down, the platform keeps the 0.2.
		{gross: 1001, bps: 2000, commission: 201, net: 800},
		{gross: 1, bps: 2000, commission: 1, net: 0},
		{gross: 700, bps: 0, commission: 0, net: 700},
		{gross: 0, bps: 2000, commission: 0, net: 0},
	}

	for _, c := range cases {
		commission, net := domain.Split(c.gross, c.bps)
		if commission != c.commission || net != c.net {
			t.Errorf("Split(%d, %d) = %d + %d, want %d + %d", c.gross, c.bps, commission, net, c.commission, c.net)
		}
		if commission+net != c.gross {
			t.Errorf("Split(%d, %d) loses or invents money: %d + %d", c.gross, c.bps, commission, net)
		}
	}
}

func TestTransitions(t *testing.T) {
	legal := [][2]domain.Status{
		{domain.StatusAuthorizing, domain.StatusAuthorized},
		{domain.StatusAuthorizing, domain.StatusRequiresAction},
		{domain.StatusRequiresAction, domain.StatusAuthorized},
		{domain.StatusAuthorized, domain.StatusCaptured},
		{domain.StatusAuthorized, domain.StatusReleased},
		{domain.StatusCaptured, domain.StatusRefunded},
		{domain.StatusReleased, domain.StatusAuthorizing},
		// A refund that never reached the card.
		{domain.StatusRefunded, domain.StatusCaptured},
	}
	for _, move := range legal {
		if !domain.CanTransition(move[0], move[1]) {
			t.Errorf("%s -> %s should be legal", move[0], move[1])
		}
	}

	// The ones that would lose or invent money.
	illegal := [][2]domain.Status{
		{domain.StatusAuthorizing, domain.StatusCaptured},    // capture what was never held
		{domain.StatusRequiresAction, domain.StatusCaptured}, // capture before the rider authenticated
		{domain.StatusCaptured, domain.StatusReleased},       // "release" money already taken
		{domain.StatusAuthorized, domain.StatusFailed},       // forget a hold that exists
		{domain.StatusRefunded, domain.StatusReleased},       // release money already given back
	}
	for _, move := range illegal {
		if domain.CanTransition(move[0], move[1]) {
			t.Errorf("%s -> %s should be refused", move[0], move[1])
		}
	}

	payment := &domain.Payment{Status: domain.StatusCaptured}
	if err := payment.Transition(domain.StatusCaptured, time.Now()); err != nil {
		t.Errorf("a repeated capture should be a no-op: %v", err)
	}
	if err := payment.Transition(domain.StatusReleased, time.Now()); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Errorf("want ErrInvalidTransition, got %v", err)
	}
}

// Keys are derived, so a retry names the same operation, and a new attempt
// names a new one.
func TestIdempotencyKeys(t *testing.T) {
	first := &domain.Payment{TripID: "trip-1", Attempt: 1}
	// A retry is the same payment loaded again, not the same value in memory.
	retried := &domain.Payment{TripID: "trip-1", Attempt: 1}
	second := &domain.Payment{TripID: "trip-1", Attempt: 2}

	if first.IdempotencyKey("capture") != retried.IdempotencyKey("capture") {
		t.Error("the same operation produced two keys")
	}
	if first.IdempotencyKey("authorize") == second.IdempotencyKey("authorize") {
		t.Error("a second attempt reuses the first attempt's key, and would get its answer")
	}
	if first.IdempotencyKey("authorize") == first.IdempotencyKey("capture") {
		t.Error("two operations share a key")
	}
}

func TestCaptureAndTransferBalance(t *testing.T) {
	payment := &domain.Payment{ID: "pay-1", TripID: "trip-1", Attempt: 1, DriverID: "drv-1",
		CapturedCents: 1450, Currency: "eur"}
	earning := domain.NewEarning(payment, 2000, time.Now())

	for _, txn := range []domain.Txn{domain.CaptureTxn(payment, earning), domain.TransferTxn(earning)} {
		if err := txn.Validate(); err != nil {
			t.Errorf("%s: %v", txn.Kind, err)
		}
	}

	unbalanced := domain.Txn{ID: "x", Entries: []domain.Entry{{Account: "a", AmountCents: 5}, {Account: "b", AmountCents: -4}}}
	if err := unbalanced.Validate(); !errors.Is(err, domain.ErrUnbalanced) {
		t.Errorf("a transaction that creates a cent passed: %v", err)
	}
	twice := domain.Txn{ID: "y", Entries: []domain.Entry{{Account: "a", AmountCents: 5}, {Account: "a", AmountCents: -5}}}
	if err := twice.Validate(); !errors.Is(err, domain.ErrUnbalanced) {
		t.Errorf("a transaction naming one account twice passed: %v", err)
	}
}

func TestFactsOf(t *testing.T) {
	cases := []struct {
		from, to domain.Status
		want     domain.FactKind
	}{
		{"", domain.StatusAuthorizing, ""},
		{"", domain.StatusFailed, domain.FactFailed},
		{domain.StatusAuthorizing, domain.StatusAuthorized, domain.FactAuthorized},
		{domain.StatusAuthorizing, domain.StatusRequiresAction, domain.FactActionRequired},
		{domain.StatusRequiresAction, domain.StatusAuthorized, domain.FactAuthorized},
		{domain.StatusAuthorized, domain.StatusCaptured, domain.FactCaptured},
		{domain.StatusAuthorized, domain.StatusReleased, ""},
		{domain.StatusAuthorized, domain.StatusAuthorized, ""},
	}
	for _, c := range cases {
		facts := domain.FactsOf(&domain.Payment{Status: c.to}, c.from)
		switch {
		case c.want == "" && len(facts) != 0:
			t.Errorf("%q -> %s: want no fact, got %s", c.from, c.to, facts[0].Kind)
		case c.want != "" && (len(facts) != 1 || facts[0].Kind != c.want):
			t.Errorf("%q -> %s: want %s, got %v", c.from, c.to, c.want, facts)
		}
	}
}
