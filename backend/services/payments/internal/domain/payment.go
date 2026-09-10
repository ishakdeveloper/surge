// Package domain is money: what a hold on a rider's card is, what may become of
// it, and how a fare divides between a driver and the platform.
//
// Nothing here imports Stripe, Postgres or Kafka. The rules below are the ones
// worth being sure about — a hold captured twice, a driver paid for a ride that
// was never charged — and they are tested by calling functions.
package domain

import (
	"errors"
	"fmt"
	"time"
)

// Status is where a payment is.
type Status string

const (
	// StatusAuthorizing means a hold has been asked for and not yet answered.
	// Stored before the processor is asked, so a crash mid-request leaves a
	// record that a hold may exist rather than a hold nobody knows about.
	StatusAuthorizing Status = "authorizing"
	// StatusRequiresAction is a hold waiting on the rider: a 3-D Secure
	// challenge.
	StatusRequiresAction Status = "requires_action"
	// StatusAuthorized is money held on the rider's card and not yet taken.
	StatusAuthorized Status = "authorized"
	// StatusCaptured is the ride paid for.
	StatusCaptured Status = "captured"
	// StatusReleased is a hold let go: the trip was cancelled, or nobody took
	// it.
	StatusReleased Status = "released"
	// StatusFailed is a hold that never happened. FailureReason says why.
	StatusFailed Status = "failed"
	// StatusRefunded is a capture given back in full.
	StatusRefunded Status = "refunded"
)

// transitions is the state machine, written down once, as data.
var transitions = map[Status][]Status{
	StatusAuthorizing:    {StatusRequiresAction, StatusAuthorized, StatusFailed, StatusReleased},
	StatusRequiresAction: {StatusAuthorized, StatusFailed, StatusReleased},
	// No path from authorized back to failed: once money is held, what
	// remains is to take it or let it go.
	StatusAuthorized: {StatusCaptured, StatusReleased},
	StatusCaptured:   {StatusRefunded},
	// Back to captured when the refund that emptied it could not reach the
	// rider's card: the money is the platform's again, and refundable again.
	StatusRefunded: {StatusCaptured},
	// A released or failed hold is finished, but its trip may be requested
	// again. That is a fresh attempt at a hold, not a revival of the old one.
	StatusReleased: {StatusAuthorizing},
	StatusFailed:   {StatusAuthorizing},
}

// Failure reasons, a closed set: they are metric labels, and the rider is told
// something specific rather than "payment failed".
const (
	FailureNoPaymentMethod      = "no_payment_method"
	FailureDeclined             = "declined"
	FailureAuthenticationFailed = "authentication_failed"
	FailureExpired              = "expired"
)

var (
	// ErrInvalidTransition is a move the state machine refuses — usually a
	// late or repeated message, not a bug.
	ErrInvalidTransition = errors.New("payments: invalid transition")
	ErrNotFound          = errors.New("payments: not found")
	// ErrConflict is a write that lost a race: what it was decided against
	// changed before it landed.
	ErrConflict = errors.New("payments: changed concurrently")
)

// CanTransition reports whether a move is legal.
func CanTransition(from, to Status) bool {
	for _, allowed := range transitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// Payment is one trip's hold, and what became of it.
type Payment struct {
	ID     string
	TripID string
	// Attempt numbers a trip's holds. It is part of every idempotency key, so a
	// second hold for a re-requested trip is a new request to the processor
	// rather than a replay of the first.
	Attempt  int
	RiderID  string
	DriverID string
	Status   Status

	AmountCents     int64
	CapturedCents   int64
	RefundedCents   int64
	CommissionCents int64
	Currency        string

	ProcessorPaymentID string
	ProcessorChargeID  string
	// ClientSecret lets the rider's browser finish an authentication step. Held
	// only while there is one to finish.
	ClientSecret  string
	FailureReason string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Transition moves the payment, or explains why it cannot. Asking for the
// status it is already in is a no-op, because at-least-once delivery means the
// same answer arrives twice.
func (p *Payment) Transition(to Status, now time.Time) error {
	if p.Status == to {
		return nil
	}
	if !CanTransition(p.Status, to) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, p.Status, to)
	}
	p.Status = to
	p.UpdatedAt = now
	return nil
}

// Open reports whether the payment holds, or is trying to hold, money on the
// rider's card — the ones a cancelled trip must let go of.
func (p *Payment) Open() bool {
	switch p.Status {
	case StatusAuthorizing, StatusRequiresAction, StatusAuthorized:
		return true
	default:
		return false
	}
}

// IdempotencyKey names one processor operation on this payment.
//
// Derived, never random. A retry after a crash must send the key the first try
// sent, so the processor answers with the first try's result instead of doing
// it again — a second hold, a second capture, a driver paid twice.
func (p *Payment) IdempotencyKey(operation string) string {
	return fmt.Sprintf("trip:%s:%d:%s", p.TripID, p.Attempt, operation)
}

// FactKind is a change the trip service acts on.
type FactKind string

const (
	FactAuthorized     FactKind = "authorized"
	FactActionRequired FactKind = "action_required"
	FactFailed         FactKind = "failed"
	FactCaptured       FactKind = "captured"
)

// Fact is a payment change worth telling the trip service, with the payment as
// it stood once the change was made.
type Fact struct {
	Kind    FactKind
	Payment Payment
}

// FactsOf returns the facts made by moving a payment from `from` to the status
// it is in now. An empty `from` means the payment is new.
func FactsOf(payment *Payment, from Status) []Fact {
	if from == payment.Status {
		return nil
	}

	var kind FactKind
	switch payment.Status {
	case StatusAuthorized:
		kind = FactAuthorized
	case StatusRequiresAction:
		kind = FactActionRequired
	case StatusFailed:
		kind = FactFailed
	case StatusCaptured:
		kind = FactCaptured
	default:
		// Authorizing, released and refunded are this service's business. The
		// trip already knows it was cancelled; it does not need telling that
		// the hold went with it.
		return nil
	}
	return []Fact{{Kind: kind, Payment: *payment}}
}
