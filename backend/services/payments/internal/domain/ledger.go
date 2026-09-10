package domain

import (
	"errors"
	"fmt"
)

// Split divides a fare between the platform and the driver.
//
// The driver's share is rounded down and the platform keeps the remainder, so
// rounding can cost the platform a cent and never the other way: a driver is
// never paid money that was not charged.
func Split(grossCents int64, commissionBps int) (commissionCents, netCents int64) {
	netCents = grossCents * int64(10_000-commissionBps) / 10_000
	return grossCents - netCents, netCents
}

// Accounts in the ledger.
const (
	// AccountClearing is money the processor holds on the platform's behalf:
	// captured from riders and not yet moved to a driver.
	AccountClearing = "processor:clearing"
	// AccountRevenue is the platform's commission.
	AccountRevenue = "platform:revenue"
)

// DriverAccount is what the platform owes a driver. Its balance is negative
// while earnings wait to be transferred, and zero once they have been.
func DriverAccount(driverID string) string { return "driver:" + driverID }

// TxnKind names what a transaction records.
type TxnKind string

const (
	TxnCapture  TxnKind = "capture"
	TxnTransfer TxnKind = "transfer"
	TxnRefund   TxnKind = "refund"
	TxnReversal TxnKind = "reversal"
	TxnDispute  TxnKind = "dispute"
)

// Entry is one leg of a transaction. Positive is a debit, negative a credit.
type Entry struct {
	Account     string
	AmountCents int64
}

// Txn is a set of entries that sum to zero — money moved, never created.
type Txn struct {
	// ID is deterministic, derived from what the transaction records, so the
	// same capture recorded twice collides instead of counting twice.
	ID       string
	Kind     TxnKind
	TripID   string
	Currency string
	Entries  []Entry
}

// ErrUnbalanced is a transaction that would create or destroy money.
var ErrUnbalanced = errors.New("payments: transaction does not balance")

// Validate refuses a transaction that does not balance, or that names an
// account twice.
func (t Txn) Validate() error {
	if t.ID == "" || len(t.Entries) < 2 {
		return fmt.Errorf("%w: %q has %d entries", ErrUnbalanced, t.ID, len(t.Entries))
	}

	var sum int64
	seen := make(map[string]bool, len(t.Entries))
	for _, entry := range t.Entries {
		if seen[entry.Account] {
			return fmt.Errorf("%w: %s names %s twice", ErrUnbalanced, t.ID, entry.Account)
		}
		seen[entry.Account] = true
		sum += entry.AmountCents
	}
	if sum != 0 {
		return fmt.Errorf("%w: %s sums to %d", ErrUnbalanced, t.ID, sum)
	}
	return nil
}

// CaptureTxn records a captured fare: the processor now holds the gross on the
// platform's behalf, and it is owed onward — the commission to the platform,
// the rest to the driver.
func CaptureTxn(payment *Payment, earning Earning) Txn {
	return Txn{
		ID:       fmt.Sprintf("capture:%s:%d", payment.TripID, payment.Attempt),
		Kind:     TxnCapture,
		TripID:   payment.TripID,
		Currency: payment.Currency,
		Entries: []Entry{
			{Account: AccountClearing, AmountCents: earning.GrossCents},
			{Account: AccountRevenue, AmountCents: -earning.CommissionCents},
			{Account: DriverAccount(earning.DriverID), AmountCents: -earning.NetCents},
		},
	}
}

// TransferTxn records a driver's share leaving for their account: the debt to
// them is settled, out of what the processor was holding.
func TransferTxn(earning Earning) Txn {
	return Txn{
		ID:       "transfer:" + earning.TripID,
		Kind:     TxnTransfer,
		TripID:   earning.TripID,
		Currency: earning.Currency,
		Entries: []Entry{
			{Account: DriverAccount(earning.DriverID), AmountCents: earning.Payable()},
			{Account: AccountClearing, AmountCents: -earning.Payable()},
		},
	}
}
