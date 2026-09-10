package domain

import (
	"fmt"
	"time"
)

// ReversalKind is how a refund took the driver's share back.
type ReversalKind string

const (
	// ReversalNone: there was nothing earned on the trip to take back.
	ReversalNone ReversalKind = "none"
	// ReversalDeducted: the share had not been paid out, so the driver is
	// simply owed less.
	ReversalDeducted ReversalKind = "deducted"
	// ReversalReversed: the share was taken back from the driver's balance.
	ReversalReversed ReversalKind = "reversed"
	// ReversalFailed: the driver's balance could not cover it — they had
	// already withdrawn it. The platform carries the loss, as the losses
	// collector it chose to be.
	ReversalFailed ReversalKind = "failed"
)

// RefundStatus is whether a refund reached the rider.
type RefundStatus string

const (
	RefundSucceeded RefundStatus = "succeeded"
	// RefundFailed is a refund the processor could not deliver. The money is
	// back with the platform, and what the refund took has been put back.
	RefundFailed RefundStatus = "failed"
)

// Refund is money given back to a rider for a captured trip.
type Refund struct {
	Status        RefundStatus
	FailureReason string
	// ProcessorRestoreTransferID is the transfer that gave a reversed driver
	// share back when the refund failed.
	ProcessorRestoreTransferID string

	ID        string
	TripID    string
	PaymentID string
	// AmountCents is given back to the rider.
	AmountCents int64
	// DriverCents is the driver's part of it: their share of the fare, in
	// proportion.
	DriverCents int64
	Currency    string
	Reason      string
	Reversal    ReversalKind

	ProcessorRefundID   string
	ProcessorReversalID string
	IdempotencyKey      string
	CreatedAt           time.Time
}

// TakesFromEarning reports whether the refund reduced what the driver keeps.
// A failed reversal did not: the driver still holds the money, and the
// platform is owed it.
func (r Refund) TakesFromEarning() bool {
	return r.Reversal == ReversalDeducted || r.Reversal == ReversalReversed
}

// DriverShareOf is the part of a refund that comes out of a driver's earning:
// their proportion of the fare, rounded down.
//
// Rounded in the driver's favour, the mirror of Split: the platform absorbs the
// cent, so a driver is never charged more than their share of what was given
// back. Bounded by what is left of the earning, so refunds in parts cannot
// together take back more than the driver was ever due.
func DriverShareOf(earning Earning, refundCents int64) int64 {
	if earning.GrossCents <= 0 {
		return 0
	}
	share := earning.NetCents * refundCents / earning.GrossCents
	if left := earning.Payable(); share > left {
		share = left
	}
	return share
}

// RefundTxn records money going back to the rider: out of what the processor
// holds, taken from the platform's commission and the driver's share in
// proportion. When the driver's part was already paid out, their account is
// left owing it until a reversal settles it.
func RefundTxn(refund Refund, driverID string) Txn {
	entries := []Entry{
		{Account: AccountClearing, AmountCents: -refund.AmountCents},
		{Account: AccountRevenue, AmountCents: refund.AmountCents - refund.DriverCents},
	}
	if driverID != "" {
		entries = append(entries, Entry{Account: DriverAccount(driverID), AmountCents: refund.DriverCents})
	}
	return Txn{
		ID: "refund:" + refund.ID, Kind: TxnRefund, TripID: refund.TripID,
		Currency: refund.Currency, Entries: entries,
	}
}

// ReversalTxn records the driver's part coming back from their balance, which
// settles what RefundTxn left them owing.
func ReversalTxn(refund Refund, driverID string) Txn {
	return Txn{
		ID: "reversal:" + refund.ID, Kind: TxnReversal, TripID: refund.TripID, Currency: refund.Currency,
		Entries: []Entry{
			{Account: DriverAccount(driverID), AmountCents: -refund.DriverCents},
			{Account: AccountClearing, AmountCents: refund.DriverCents},
		},
	}
}

// RefundFailedTxn is exactly the opposite of RefundTxn: the money is back with
// the processor, the commission is the platform's again, and the driver's
// share is theirs again.
func RefundFailedTxn(refund Refund, driverID string) Txn {
	txn := RefundTxn(refund, driverID)
	txn.ID, txn.Kind = "refund-failed:"+refund.ID, TxnRefund
	for i := range txn.Entries {
		txn.Entries[i].AmountCents = -txn.Entries[i].AmountCents
	}
	return txn
}

// RefundRestoreTxn records a reversed share going back to the driver after
// the refund it was reversed for failed.
func RefundRestoreTxn(refund Refund, driverID string) Txn {
	return Txn{
		ID: "refund-restore:" + refund.ID, Kind: TxnTransfer, TripID: refund.TripID, Currency: refund.Currency,
		Entries: []Entry{
			{Account: DriverAccount(driverID), AmountCents: refund.DriverCents},
			{Account: AccountClearing, AmountCents: -refund.DriverCents},
		},
	}
}

// RefundKey is the processor idempotency key for a refund, derived from the
// trip and the key ops sent, so the retried click is the same refund at
// Stripe as it is here.
func RefundKey(tripID, key string) string { return fmt.Sprintf("trip:%s:refund:%s", tripID, key) }
