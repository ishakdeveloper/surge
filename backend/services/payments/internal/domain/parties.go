package domain

import (
	"context"
	"time"
)

// Card is what a rider is shown of the card that will be held.
type Card struct {
	Brand    string
	Last4    string
	ExpMonth int
	ExpYear  int
}

// Customer is a rider as the processor knows them.
type Customer struct {
	UserID              string
	ProcessorCustomerID string
	// PaymentMethodID is the card holds are placed on. Empty until the rider
	// has saved one.
	PaymentMethodID string
	Card            Card
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// CanPay reports whether there is a card to hold.
func (c Customer) CanPay() bool { return c.PaymentMethodID != "" }

// TransfersActive is the processor's word for an account that can be paid.
const TransfersActive = "active"

// PayoutAccount is a driver's connected account.
type PayoutAccount struct {
	DriverID           string
	ProcessorAccountID string
	// TransfersStatus mirrors the processor's capability status for receiving
	// transfers. Only TransfersActive may be paid.
	TransfersStatus string
	RequirementsDue bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// CanReceive reports whether money may be sent to this account now.
func (a PayoutAccount) CanReceive() bool { return a.TransfersStatus == TransfersActive }

// EarningStatus is where a driver's share of a trip is.
type EarningStatus string

const (
	// EarningUnpaid is owed, waiting for an account that can receive it.
	EarningUnpaid EarningStatus = "unpaid"
	// EarningTransferred has been moved to the driver's account.
	EarningTransferred EarningStatus = "transferred"
	// EarningReversed was taken back after a refund or a dispute.
	EarningReversed EarningStatus = "reversed"
)

// Earning is a driver's share of one captured trip.
type Earning struct {
	TripID              string
	DriverID            string
	PaymentID           string
	GrossCents          int64
	CommissionCents     int64
	NetCents            int64
	Currency            string
	Status              EarningStatus
	ProcessorTransferID string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// NewEarning divides a captured payment into what the driver earned.
func NewEarning(payment *Payment, commissionBps int, now time.Time) Earning {
	commission, net := Split(payment.CapturedCents, commissionBps)
	return Earning{
		TripID:          payment.TripID,
		DriverID:        payment.DriverID,
		PaymentID:       payment.ID,
		GrossCents:      payment.CapturedCents,
		CommissionCents: commission,
		NetCents:        net,
		Currency:        payment.Currency,
		Status:          EarningUnpaid,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

// DefaultCurrency is the one market's. Totals are only meaningful within a
// currency, and until there is a second one this is the currency they are in.
const DefaultCurrency = "eur"

// EarningFilter pages a driver's earnings, newest first.
type EarningFilter struct {
	DriverID string
	Limit    int
	// Cursor is the trip id of the last earning on the previous page.
	Cursor string
}

type EarningPage struct {
	Earnings []Earning
	// NextCursor is empty when there are no more.
	NextCursor string
}

// EarningTotals is what a driver has earned, split by whether it has reached
// them.
type EarningTotals struct {
	OwedCents int64
	PaidCents int64
}

// Change is one atomic write: a payment moved, and everything that moves with
// it.
//
// One method rather than a method per combination, because the combinations
// are the point. A capture that stored the earning but not its ledger entries
// is a driver owed money from nowhere, and one transaction is the only way to
// make "all of it or none of it" true in every repository.
type Change struct {
	// Payment is written with a compare-and-set on From. An empty From means
	// the payment is new; a second insert for the same trip is ErrConflict.
	Payment *Payment
	From    Status

	// Earning is written with a compare-and-set on EarningFrom, the same way.
	Earning     *Earning
	EarningFrom EarningStatus

	Txns  []Txn
	Facts []Fact
}

// Repository is persistence, as the service sees it.
type Repository interface {
	Customer(ctx context.Context, userID string) (Customer, error)
	SaveCustomer(ctx context.Context, customer Customer) error

	PaymentForTrip(ctx context.Context, tripID string) (*Payment, error)
	PaymentByProcessorID(ctx context.Context, processorPaymentID string) (*Payment, error)

	PayoutAccount(ctx context.Context, driverID string) (PayoutAccount, error)
	SavePayoutAccount(ctx context.Context, account PayoutAccount) error

	// The processor names customers and accounts by its own ids in webhooks.
	CustomerByProcessorID(ctx context.Context, processorCustomerID string) (Customer, error)
	PayoutAccountByProcessorID(ctx context.Context, processorAccountID string) (PayoutAccount, error)

	Earning(ctx context.Context, tripID string) (*Earning, error)
	UnpaidEarnings(ctx context.Context, driverID string) ([]Earning, error)
	ListEarnings(ctx context.Context, filter EarningFilter) (EarningPage, error)
	EarningTotals(ctx context.Context, driverID string) (EarningTotals, error)

	// LedgerBalance sums an account's entries.
	LedgerBalance(ctx context.Context, account string) (int64, error)

	// Apply writes a change atomically, or returns ErrConflict and writes
	// nothing when anything it was decided against has moved.
	Apply(ctx context.Context, change Change) error
}
