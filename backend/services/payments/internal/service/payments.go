// Package service is what payments does with a trip's lifecycle: hold on a
// request, capture on completion, let go on a cancel, and pass the driver's
// share on once there is somewhere to send it.
//
// It depends on domain interfaces and a Processor, and nothing else. Every test
// runs against the in-memory repository and the fake processor, so "a redelivered
// completion does not capture twice" is checked in milliseconds, with no Stripe
// account and no container.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
)

// Processor is the payment processor, as this service needs it: Stripe in
// production, a deterministic fake in tests and on a laptop.
//
// Every mutating call carries an idempotency key the caller derived, so a call
// repeated after a crash is answered with the first call's result rather than
// performed again.
type Processor interface {
	// Authorize places a hold. A decline or an authentication step is an
	// Outcome, not an error: errors mean the processor could not be asked, and
	// are retried.
	Authorize(ctx context.Context, request AuthorizeRequest) (Authorization, error)
	Capture(ctx context.Context, request CaptureRequest) (Capture, error)
	Release(ctx context.Context, processorPaymentID, idempotencyKey string) error
	Transfer(ctx context.Context, request TransferRequest) (transferID string, err error)

	// EnsureCustomer creates the processor's record of a rider.
	EnsureCustomer(ctx context.Context, userID, email, idempotencyKey string) (customerID string, err error)
	// CreateSetupIntent starts saving a card for a customer.
	CreateSetupIntent(ctx context.Context, customerID string) (SetupIntent, error)
	// CreateConnectedAccount creates the account a driver is paid into.
	CreateConnectedAccount(ctx context.Context, driverID, email, idempotencyKey string) (ConnectedAccount, error)
	// OnboardingLink is a single-use link into the processor's hosted
	// onboarding for an account.
	OnboardingLink(ctx context.Context, accountID, refreshURL, returnURL string) (string, error)

	// Refund gives money back to a rider from a captured payment.
	Refund(ctx context.Context, request RefundRequest) (refundID string, err error)
	// ReverseTransfer takes part of a transfer back from a driver's balance.
	// ErrReversalRefused when the balance cannot cover it.
	ReverseTransfer(ctx context.Context, request ReversalRequest) (reversalID string, err error)
	// Balance is what a connected account holds.
	Balance(ctx context.Context, accountID string) (Balance, error)
	// Payout sends money from a connected account's balance to its bank.
	// ErrPayoutRefused, carrying the processor's reason, when it will not.
	Payout(ctx context.Context, request PayoutRequest) (Payout, error)
}

// Outcome is how a request for a hold was answered.
type Outcome string

const (
	OutcomeAuthorized     Outcome = "authorized"
	OutcomeActionRequired Outcome = "action_required"
	OutcomeDeclined       Outcome = "declined"
)

type AuthorizeRequest struct {
	CustomerID      string
	PaymentMethodID string
	AmountCents     int64
	Currency        string
	// TripID groups the charge with the transfer that later pays the driver
	// out of it.
	TripID         string
	PaymentID      string
	IdempotencyKey string
}

type Authorization struct {
	ProcessorPaymentID string
	Outcome            Outcome
	// ClientSecret is set when the rider has an authentication step to finish.
	ClientSecret string
	// DeclineReason is one of domain.Failure*, set when declined.
	DeclineReason string
}

type CaptureRequest struct {
	ProcessorPaymentID string
	AmountCents        int64
	IdempotencyKey     string
}

type Capture struct {
	// ProcessorChargeID is what the driver's transfer is funded from.
	ProcessorChargeID string
}

type TransferRequest struct {
	DestinationAccountID string
	AmountCents          int64
	Currency             string
	// SourceChargeID ties the transfer to the rider's charge, so it waits for
	// that charge's funds rather than failing on an empty balance.
	SourceChargeID string
	TripID         string
	IdempotencyKey string
}

// Trip is what this service knows of a trip: what its lifecycle facts carry.
type Trip struct {
	ID         string
	RiderID    string
	DriverID   string
	TotalCents int64
	Currency   string
}

// ErrUnheld is a trip that completed or ended with no payment behind it —
// booked while payments was not in the loop. There is nothing to capture, and
// inventing a charge after the ride would be charging a rider who was never
// asked.
var ErrUnheld = errors.New("service: trip has no payment")

type Service struct {
	repo          domain.Repository
	processor     Processor
	commissionBps int
	webURL        string
	now           func() time.Time
	newID         func() string
}

type Options struct {
	Repository domain.Repository
	Processor  Processor
	// CommissionBps is the platform's share of a fare in basis points: 2000 is
	// 20%.
	CommissionBps int
	// WebURL is the web app's origin, which onboarding links send a driver
	// back to.
	WebURL string
	Now    func() time.Time
	NewID  func() string
}

func New(options Options) (*Service, error) {
	// Below 100%: a commission of everything leaves a driver working for
	// nothing, and is a misconfiguration rather than a business model.
	if options.CommissionBps < 0 || options.CommissionBps >= 10_000 {
		return nil, fmt.Errorf("service: a commission of %d basis points is not a share of a fare", options.CommissionBps)
	}
	service := &Service{
		repo:          options.Repository,
		processor:     options.Processor,
		commissionBps: options.CommissionBps,
		webURL:        options.WebURL,
		now:           options.Now,
		newID:         options.NewID,
	}
	if service.now == nil {
		service.now = time.Now
	}
	if service.newID == nil {
		service.newID = uuid.NewString
	}
	return service, nil
}

// TripRequested places a hold for the fare.
func (s *Service) TripRequested(ctx context.Context, trip Trip) error {
	existing, err := s.repo.PaymentForTrip(ctx, trip.ID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}

	now := s.now()
	var (
		payment *domain.Payment
		from    domain.Status
	)

	switch {
	case existing == nil:
		payment = &domain.Payment{
			ID: s.newID(), TripID: trip.ID, Attempt: 1, RiderID: trip.RiderID,
			Status: domain.StatusAuthorizing, AmountCents: trip.TotalCents, Currency: trip.Currency,
			CreatedAt: now, UpdatedAt: now,
		}

	case existing.Status == domain.StatusAuthorizing:
		// Delivered again after the hold was asked for and before its answer
		// was stored: a crash in between. Ask again under the same key, and
		// the processor answers with the first request's hold rather than
		// placing a second.
		return s.authorize(ctx, existing)

	case existing.Status == domain.StatusReleased, existing.Status == domain.StatusFailed:
		// The same trip, requested again after its last hold was let go or
		// never happened. A fresh attempt, with fresh keys.
		from = existing.Status
		payment = existing
		if err := payment.Transition(domain.StatusAuthorizing, now); err != nil {
			return err
		}
		payment.Attempt++
		payment.AmountCents = trip.TotalCents
		payment.ProcessorPaymentID, payment.ClientSecret, payment.FailureReason = "", "", ""

	default:
		// Held, waiting on the rider, or already paid: a redelivery.
		return nil
	}

	// Stored before the processor is asked. A crash mid-request then leaves a
	// payment that says a hold may exist, which the redelivery finishes, rather
	// than a hold on a rider's card that nothing here knows about.
	if err := s.repo.Apply(ctx, domain.Change{Payment: payment, From: from}); err != nil {
		return err
	}
	return s.authorize(ctx, payment)
}

// authorize asks the processor for a hold on a payment that is authorizing,
// and stores the answer.
func (s *Service) authorize(ctx context.Context, payment *domain.Payment) error {
	customer, err := s.repo.Customer(ctx, payment.RiderID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	if err != nil || !customer.CanPay() {
		// No card to hold. Stored as a failed payment rather than refused, so
		// the rider can be told why and the trip is cancelled by its fact.
		return s.settle(ctx, payment, Authorization{
			Outcome: OutcomeDeclined, DeclineReason: domain.FailureNoPaymentMethod,
		})
	}

	answer, err := s.processor.Authorize(ctx, AuthorizeRequest{
		CustomerID:      customer.ProcessorCustomerID,
		PaymentMethodID: customer.PaymentMethodID,
		AmountCents:     payment.AmountCents,
		Currency:        payment.Currency,
		TripID:          payment.TripID,
		PaymentID:       payment.ID,
		IdempotencyKey:  payment.IdempotencyKey("authorize"),
	})
	if err != nil {
		return fmt.Errorf("service: authorize trip %s: %w", payment.TripID, err)
	}
	return s.settle(ctx, payment, answer)
}

// settle stores what a request for a hold came to.
func (s *Service) settle(ctx context.Context, payment *domain.Payment, answer Authorization) error {
	from := payment.Status
	next := *payment

	var to domain.Status
	switch answer.Outcome {
	case OutcomeAuthorized:
		to, next.ClientSecret = domain.StatusAuthorized, ""
	case OutcomeActionRequired:
		to, next.ClientSecret = domain.StatusRequiresAction, answer.ClientSecret
	case OutcomeDeclined:
		to, next.ClientSecret = domain.StatusFailed, ""
		next.FailureReason = answer.DeclineReason
		if next.FailureReason == "" {
			next.FailureReason = domain.FailureDeclined
		}
	default:
		return fmt.Errorf("service: the processor answered %q", answer.Outcome)
	}
	if answer.ProcessorPaymentID != "" {
		next.ProcessorPaymentID = answer.ProcessorPaymentID
	}

	if err := next.Transition(to, s.now()); err != nil {
		return err
	}
	return s.repo.Apply(ctx, domain.Change{Payment: &next, From: from, Facts: domain.FactsOf(&next, from)})
}

// AuthorizationSettled applies what the processor reported about a hold after
// the fact: the rider finishing a 3-D Secure challenge, or failing it.
func (s *Service) AuthorizationSettled(ctx context.Context, processorPaymentID string, answer Authorization) error {
	payment, err := s.repo.PaymentByProcessorID(ctx, processorPaymentID)
	if err != nil {
		return err
	}
	if payment.Status != domain.StatusAuthorizing && payment.Status != domain.StatusRequiresAction {
		// Already settled. Webhooks arrive late and more than once, and the
		// synchronous answer usually beats them.
		return nil
	}
	return s.settle(ctx, payment, answer)
}

// TripCompleted takes the fare, and passes the driver's share on.
func (s *Service) TripCompleted(ctx context.Context, trip Trip) error {
	payment, err := s.repo.PaymentForTrip(ctx, trip.ID)
	if errors.Is(err, domain.ErrNotFound) {
		return fmt.Errorf("%w: %s completed", ErrUnheld, trip.ID)
	}
	if err != nil {
		return err
	}

	switch payment.Status {
	case domain.StatusAuthorized:
		return s.capture(ctx, payment, trip.DriverID)
	case domain.StatusCaptured, domain.StatusRefunded:
		// A redelivered completion. Nothing to take, but the driver's share
		// may not have gone out before the crash that caused the redelivery.
		return s.payDriver(ctx, trip.ID)
	default:
		return fmt.Errorf("%w: trip %s completed with its payment %s",
			domain.ErrInvalidTransition, trip.ID, payment.Status)
	}
}

func (s *Service) capture(ctx context.Context, payment *domain.Payment, driverID string) error {
	result, err := s.processor.Capture(ctx, CaptureRequest{
		ProcessorPaymentID: payment.ProcessorPaymentID,
		AmountCents:        payment.AmountCents,
		IdempotencyKey:     payment.IdempotencyKey("capture"),
	})
	if err != nil {
		return fmt.Errorf("service: capture trip %s: %w", payment.TripID, err)
	}

	now := s.now()
	from := payment.Status
	next := *payment
	next.DriverID = driverID
	next.ProcessorChargeID = result.ProcessorChargeID
	next.CapturedCents = payment.AmountCents
	if err := next.Transition(domain.StatusCaptured, now); err != nil {
		return err
	}

	earning := domain.NewEarning(&next, s.commissionBps, now)
	next.CommissionCents = earning.CommissionCents

	// The capture, what the driver earned, and the ledger entries saying
	// where the money now sits: one write, so none of them exists without
	// the others.
	if err := s.repo.Apply(ctx, domain.Change{
		Payment: &next, From: from,
		Earning: &earning,
		Txns:    []domain.Txn{domain.CaptureTxn(&next, earning)},
		Facts:   domain.FactsOf(&next, from),
	}); err != nil {
		return err
	}
	return s.payDriver(ctx, next.TripID)
}

// payDriver moves a trip's earning to its driver, if their account can
// receive it yet.
//
// An earning a driver cannot receive stays unpaid, and goes out the moment
// their account can: the ride is paid for either way, and the rider's charge
// is not held hostage to the driver's paperwork.
func (s *Service) payDriver(ctx context.Context, tripID string) error {
	earning, err := s.repo.Earning(ctx, tripID)
	if err != nil {
		return err
	}
	if earning.Status != domain.EarningUnpaid {
		return nil
	}

	account, err := s.repo.PayoutAccount(ctx, earning.DriverID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !account.CanReceive() {
		return nil
	}

	payment, err := s.repo.PaymentForTrip(ctx, tripID)
	if err != nil {
		return err
	}

	next := *earning
	next.Status = domain.EarningTransferred
	next.UpdatedAt = s.now()

	// A share that rounds to nothing is settled without troubling the
	// processor, which refuses a transfer of zero. What is paid is what is
	// payable: a refund before payout has already taken its part.
	if earning.Payable() > 0 {
		transferID, err := s.processor.Transfer(ctx, TransferRequest{
			DestinationAccountID: account.ProcessorAccountID,
			AmountCents:          earning.Payable(),
			Currency:             earning.Currency,
			SourceChargeID:       payment.ProcessorChargeID,
			TripID:               tripID,
			IdempotencyKey:       payment.IdempotencyKey("transfer"),
		})
		if err != nil {
			return fmt.Errorf("service: transfer trip %s: %w", tripID, err)
		}
		next.ProcessorTransferID = transferID
	}

	return s.repo.Apply(ctx, domain.Change{
		Earning: &next, EarningFrom: domain.EarningUnpaid,
		Txns: []domain.Txn{domain.TransferTxn(next)},
	})
}

// PayOutstanding sends a driver everything they are owed, once their account
// can receive it.
func (s *Service) PayOutstanding(ctx context.Context, driverID string) error {
	earnings, err := s.repo.UnpaidEarnings(ctx, driverID)
	if err != nil {
		return err
	}
	for _, earning := range earnings {
		if err := s.payDriver(ctx, earning.TripID); err != nil {
			return err
		}
	}
	return nil
}

// TripEnded lets go of the hold on a trip that was cancelled, or that nobody
// took.
func (s *Service) TripEnded(ctx context.Context, trip Trip) error {
	payment, err := s.repo.PaymentForTrip(ctx, trip.ID)
	if errors.Is(err, domain.ErrNotFound) {
		return fmt.Errorf("%w: %s ended", ErrUnheld, trip.ID)
	}
	if err != nil {
		return err
	}

	if payment.Status == domain.StatusAuthorizing {
		// Asked for and never answered, so there may be a hold nothing here
		// knows the id of. Finish asking — under the same key, so this finds
		// the existing hold rather than placing one — and release what comes
		// back.
		if err := s.authorize(ctx, payment); err != nil {
			return err
		}
		if payment, err = s.repo.PaymentForTrip(ctx, trip.ID); err != nil {
			return err
		}
	}
	if !payment.Open() {
		return nil
	}

	if err := s.processor.Release(ctx, payment.ProcessorPaymentID, payment.IdempotencyKey("release")); err != nil {
		return fmt.Errorf("service: release trip %s: %w", trip.ID, err)
	}

	from := payment.Status
	next := *payment
	next.ClientSecret = ""
	if err := next.Transition(domain.StatusReleased, s.now()); err != nil {
		return err
	}
	return s.repo.Apply(ctx, domain.Change{Payment: &next, From: from})
}
