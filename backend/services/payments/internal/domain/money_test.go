package domain_test

import (
	"testing"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
)

// A driver gives back their share of a refund, rounded in their favour, and
// never more in total than they were due.
func TestDriverShareOfARefund(t *testing.T) {
	earning := domain.Earning{GrossCents: 1450, NetCents: 1160}

	cases := []struct {
		refund, want int64
	}{
		{refund: 1450, want: 1160}, // all of it
		{refund: 725, want: 580},   // half
		{refund: 1, want: 0},       // 0.8 of a cent: the platform absorbs it
		{refund: 333, want: 266},   // 266.4, rounded down
	}
	for _, c := range cases {
		if got := domain.DriverShareOf(earning, c.refund); got != c.want {
			t.Errorf("refunding %d takes %d from the driver, want %d", c.refund, got, c.want)
		}
	}

	// After half has come back, a second full-size ask takes only what is left.
	earning.ReversedCents = 580
	if got := domain.DriverShareOf(earning, 1450); got != 580 {
		t.Errorf("a second refund takes %d, want the 580 left", got)
	}
}

func TestRefundAndReversalBalance(t *testing.T) {
	refund := domain.Refund{ID: "ref-1", TripID: "trip-1", AmountCents: 725, DriverCents: 580, Currency: "eur"}
	for _, txn := range []domain.Txn{domain.RefundTxn(refund, "drv-1"), domain.ReversalTxn(refund, "drv-1")} {
		if err := txn.Validate(); err != nil {
			t.Errorf("%s: %v", txn.Kind, err)
		}
	}
	// A trip with no driver share still balances.
	if err := domain.RefundTxn(domain.Refund{ID: "ref-2", AmountCents: 100}, "").Validate(); err != nil {
		t.Errorf("a refund with no driver: %v", err)
	}
}

func TestPayableIsWhatRefundsLeft(t *testing.T) {
	if got := (domain.Earning{NetCents: 1160, ReversedCents: 580}).Payable(); got != 580 {
		t.Errorf("payable %d, want 580", got)
	}
}
