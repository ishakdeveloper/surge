package domain

import "time"

// DisputeStatus is where a dispute is.
type DisputeStatus string

const (
	DisputeOpen DisputeStatus = "open"
	DisputeWon  DisputeStatus = "won"
	DisputeLost DisputeStatus = "lost"
)

// Dispute is a rider's bank taking a charge back.
//
// While it is open the money is gone from the platform, so the driver's share
// of it is taken back the way a refund takes it. Won, the money returns and so
// does the driver's share. Lost, it stays gone.
type Dispute struct {
	ID                 string
	ProcessorDisputeID string
	TripID             string
	PaymentID          string
	AmountCents        int64
	DriverCents        int64
	Currency           string
	Reason             string
	Reversal           ReversalKind
	Status             DisputeStatus

	ProcessorReversalID        string
	ProcessorRestoreTransferID string
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
}

// DisputeTxn records the disputed money leaving: out of what the processor
// holds, from the commission and the driver's share in proportion — as a
// refund does, because to the ledger it is one the bank decided on.
func DisputeTxn(dispute Dispute, driverID string) Txn {
	return disputeMove("dispute:"+dispute.ID, dispute, driverID, 1)
}

// DisputeWonTxn records the money coming back, and the driver's share owed to
// them again.
func DisputeWonTxn(dispute Dispute, driverID string) Txn {
	return disputeMove("dispute-won:"+dispute.ID, dispute, driverID, -1)
}

// disputeMove is DisputeTxn when sign is 1, and exactly its opposite when -1.
func disputeMove(id string, dispute Dispute, driverID string, sign int64) Txn {
	entries := []Entry{
		{Account: AccountClearing, AmountCents: -sign * dispute.AmountCents},
		{Account: AccountRevenue, AmountCents: sign * (dispute.AmountCents - dispute.DriverCents)},
	}
	if driverID != "" {
		entries = append(entries, Entry{Account: DriverAccount(driverID), AmountCents: sign * dispute.DriverCents})
	}
	return Txn{ID: id, Kind: TxnDispute, TripID: dispute.TripID, Currency: dispute.Currency, Entries: entries}
}

// DisputeReversalTxn records the driver's share coming back from their
// balance when the dispute opened.
func DisputeReversalTxn(dispute Dispute, driverID string) Txn {
	return Txn{
		ID: "dispute-reversal:" + dispute.ID, Kind: TxnReversal, TripID: dispute.TripID, Currency: dispute.Currency,
		Entries: []Entry{
			{Account: DriverAccount(driverID), AmountCents: -dispute.DriverCents},
			{Account: AccountClearing, AmountCents: dispute.DriverCents},
		},
	}
}

// DisputeRestoreTxn records a reversed share going back to the driver once
// the dispute was won.
func DisputeRestoreTxn(dispute Dispute, driverID string) Txn {
	return Txn{
		ID: "dispute-restore:" + dispute.ID, Kind: TxnTransfer, TripID: dispute.TripID, Currency: dispute.Currency,
		Entries: []Entry{
			{Account: DriverAccount(driverID), AmountCents: dispute.DriverCents},
			{Account: AccountClearing, AmountCents: -dispute.DriverCents},
		},
	}
}
