package service

import (
	"context"
	"errors"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
)

// SavedMethod is a card the processor has attached to a customer.
type SavedMethod struct {
	PaymentMethodID string
	Card            domain.Card
}

// SetupIntent is the start of saving a card.
type SetupIntent struct {
	// ClientSecret lets the rider's browser collect the card in the
	// processor's own form, so a card number never touches this system.
	ClientSecret string
	// Attached is set by a processor that attaches a card on the spot, as the
	// fake does. Stripe reports it later, by webhook, and leaves this nil.
	Attached *SavedMethod
}

// ConnectedAccount is a driver's account as the processor reports it.
type ConnectedAccount struct {
	ID              string
	TransfersStatus string
	RequirementsDue bool
}

// Where the processor sends a driver back to from onboarding: done, or a link
// that expired and needs making again.
const (
	onboardingReturnPath  = "/drive/payouts/return"
	onboardingRefreshPath = "/drive/payouts/refresh"
)

// SetupCard starts saving a card for a rider, creating their customer record
// first if this is the first time, and returns the secret the browser needs.
func (s *Service) SetupCard(ctx context.Context, userID, email string) (string, error) {
	customer, err := s.customer(ctx, userID, email)
	if err != nil {
		return "", err
	}

	intent, err := s.processor.CreateSetupIntent(ctx, customer.ProcessorCustomerID)
	if err != nil {
		return "", err
	}
	if intent.Attached != nil {
		if err := s.CardSaved(ctx, customer.ProcessorCustomerID, *intent.Attached); err != nil {
			return "", err
		}
	}
	return intent.ClientSecret, nil
}

func (s *Service) customer(ctx context.Context, userID, email string) (domain.Customer, error) {
	existing, err := s.repo.Customer(ctx, userID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return domain.Customer{}, err
	}

	// Keyed by the rider, so two tabs racing to save a card create one
	// customer at the processor rather than two, one of which is orphaned.
	id, err := s.processor.EnsureCustomer(ctx, userID, email, "customer:"+userID)
	if err != nil {
		return domain.Customer{}, err
	}

	now := s.now()
	customer := domain.Customer{UserID: userID, ProcessorCustomerID: id, CreatedAt: now, UpdatedAt: now}
	if err := s.repo.SaveCustomer(ctx, customer); err != nil {
		return domain.Customer{}, err
	}
	return customer, nil
}

// CardSaved records the card a rider saved, as the processor reported it. The
// latest card saved is the one holds are placed on.
func (s *Service) CardSaved(ctx context.Context, processorCustomerID string, method SavedMethod) error {
	customer, err := s.repo.CustomerByProcessorID(ctx, processorCustomerID)
	if err != nil {
		return err
	}
	customer.PaymentMethodID = method.PaymentMethodID
	customer.Card = method.Card
	customer.UpdatedAt = s.now()
	return s.repo.SaveCustomer(ctx, customer)
}

// Customer is a rider's record, or the zero value when they have none — a
// rider who has never saved a card is not an error.
func (s *Service) Customer(ctx context.Context, userID string) (domain.Customer, error) {
	customer, err := s.repo.Customer(ctx, userID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Customer{UserID: userID}, nil
	}
	return customer, err
}

// Payment is a trip's payment.
func (s *Service) Payment(ctx context.Context, tripID string) (*domain.Payment, error) {
	return s.repo.PaymentForTrip(ctx, tripID)
}

// PayoutAccount is a driver's account, and whether they have one.
func (s *Service) PayoutAccount(ctx context.Context, driverID string) (domain.PayoutAccount, bool, error) {
	account, err := s.repo.PayoutAccount(ctx, driverID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.PayoutAccount{}, false, nil
	}
	if err != nil {
		return domain.PayoutAccount{}, false, err
	}
	return account, true, nil
}

// StartOnboarding creates a driver's payout account if they have none, and
// returns a link into the processor's onboarding for it.
func (s *Service) StartOnboarding(ctx context.Context, driverID, email string) (string, error) {
	account, found, err := s.PayoutAccount(ctx, driverID)
	if err != nil {
		return "", err
	}

	if !found {
		created, err := s.processor.CreateConnectedAccount(ctx, driverID, email, "account:"+driverID)
		if err != nil {
			return "", err
		}
		now := s.now()
		account = domain.PayoutAccount{
			DriverID: driverID, ProcessorAccountID: created.ID,
			TransfersStatus: created.TransfersStatus, RequirementsDue: created.RequirementsDue,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := s.repo.SavePayoutAccount(ctx, account); err != nil {
			return "", err
		}
		// A processor that activates accounts on the spot — the fake — can be
		// paid now, and a driver who drove before onboarding is owed.
		if account.CanReceive() {
			if err := s.PayOutstanding(ctx, driverID); err != nil {
				return "", err
			}
		}
	}

	return s.processor.OnboardingLink(ctx, account.ProcessorAccountID,
		s.webURL+onboardingRefreshPath, s.webURL+onboardingReturnPath)
}

// AccountUpdated applies what the processor reported about a driver's
// account, and pays what they are owed the moment it can receive money.
func (s *Service) AccountUpdated(ctx context.Context, update ConnectedAccount) error {
	account, err := s.repo.PayoutAccountByProcessorID(ctx, update.ID)
	if err != nil {
		return err
	}
	account.TransfersStatus = update.TransfersStatus
	account.RequirementsDue = update.RequirementsDue
	account.UpdatedAt = s.now()
	if err := s.repo.SavePayoutAccount(ctx, account); err != nil {
		return err
	}
	if !account.CanReceive() {
		return nil
	}
	return s.PayOutstanding(ctx, account.DriverID)
}

// DefaultPageSize and MaxPageSize bound a listing, as the trip service's do.
const (
	DefaultPageSize = 20
	MaxPageSize     = 100
)

// Earnings is a page of a driver's earnings, and their totals across all of
// them.
func (s *Service) Earnings(ctx context.Context, driverID string, limit int, cursor string) (domain.EarningPage, domain.EarningTotals, error) {
	switch {
	case limit <= 0:
		limit = DefaultPageSize
	case limit > MaxPageSize:
		limit = MaxPageSize
	}

	page, err := s.repo.ListEarnings(ctx, domain.EarningFilter{DriverID: driverID, Limit: limit, Cursor: cursor})
	if err != nil {
		return domain.EarningPage{}, domain.EarningTotals{}, err
	}
	totals, err := s.repo.EarningTotals(ctx, driverID)
	if err != nil {
		return domain.EarningPage{}, domain.EarningTotals{}, err
	}
	return page, totals, nil
}
