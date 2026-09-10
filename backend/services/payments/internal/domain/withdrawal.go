package domain

import "time"

// WithdrawalStatus is where a driver's withdrawal is.
type WithdrawalStatus string

const (
	// WithdrawalRequested is stored before the processor is asked, so a crash
	// in between leaves a record the retried tap finishes, not a payout
	// nothing here knows about.
	WithdrawalRequested WithdrawalStatus = "requested"
	WithdrawalInTransit WithdrawalStatus = "in_transit"
	WithdrawalPaid      WithdrawalStatus = "paid"
	WithdrawalFailed    WithdrawalStatus = "failed"
)

// Withdrawal is a driver moving money from their balance to their bank.
//
// Not in the ledger. By the time a driver can withdraw it, the money is theirs
// — transferred to their account — and the ledger records what the platform
// holds and owes, which a payout from the driver's own balance changes
// neither of.
type Withdrawal struct {
	ID                string
	DriverID          string
	AmountCents       int64
	Currency          string
	Status            WithdrawalStatus
	ProcessorPayoutID string
	FailureReason     string
	IdempotencyKey    string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// WithdrawalFilter pages a driver's withdrawals, newest first.
type WithdrawalFilter struct {
	DriverID string
	Limit    int
	Cursor   string
}

type WithdrawalPage struct {
	Withdrawals []Withdrawal
	NextCursor  string
}
