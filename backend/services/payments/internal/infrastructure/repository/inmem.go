// Package repository implements domain.Repository.
//
// The in-memory one is what the service's tests run against, and it keeps the
// same promises the Postgres one does — compare-and-set, one payment per trip,
// a transaction applied twice refused — so the service's handling of a lost
// race is tested without a database.
package repository

import (
	"context"
	"sort"
	"sync"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
)

type InMemory struct {
	mu          sync.Mutex
	customers   map[string]domain.Customer
	payments    map[string]domain.Payment
	byTrip      map[string]string
	byProcessor map[string]string
	accounts    map[string]domain.PayoutAccount
	earnings    map[string]domain.Earning
	posted      map[string]bool
	balances    map[string]int64
	facts       []domain.Fact
}

func NewInMemory() *InMemory {
	return &InMemory{
		customers: map[string]domain.Customer{}, payments: map[string]domain.Payment{},
		byTrip: map[string]string{}, byProcessor: map[string]string{},
		accounts: map[string]domain.PayoutAccount{}, earnings: map[string]domain.Earning{},
		posted: map[string]bool{}, balances: map[string]int64{},
	}
}

// Facts returns every fact stored, in order: what the outbox would have
// published.
func (r *InMemory) Facts() []domain.Fact {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]domain.Fact(nil), r.facts...)
}

func (r *InMemory) Customer(_ context.Context, userID string) (domain.Customer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	customer, ok := r.customers[userID]
	if !ok {
		return domain.Customer{}, domain.ErrNotFound
	}
	return customer, nil
}

func (r *InMemory) SaveCustomer(_ context.Context, customer domain.Customer) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.customers[customer.UserID] = customer
	return nil
}

func (r *InMemory) PaymentForTrip(_ context.Context, tripID string) (*domain.Payment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.payment(r.byTrip[tripID])
}

func (r *InMemory) PaymentByProcessorID(_ context.Context, processorPaymentID string) (*domain.Payment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.payment(r.byProcessor[processorPaymentID])
}

func (r *InMemory) payment(id string) (*domain.Payment, error) {
	payment, ok := r.payments[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &payment, nil
}

func (r *InMemory) PayoutAccount(_ context.Context, driverID string) (domain.PayoutAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	account, ok := r.accounts[driverID]
	if !ok {
		return domain.PayoutAccount{}, domain.ErrNotFound
	}
	return account, nil
}

func (r *InMemory) SavePayoutAccount(_ context.Context, account domain.PayoutAccount) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.accounts[account.DriverID] = account
	return nil
}

func (r *InMemory) Earning(_ context.Context, tripID string) (*domain.Earning, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	earning, ok := r.earnings[tripID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &earning, nil
}

func (r *InMemory) UnpaidEarnings(_ context.Context, driverID string) ([]domain.Earning, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var unpaid []domain.Earning
	for _, earning := range r.earnings {
		if earning.DriverID == driverID && earning.Status == domain.EarningUnpaid {
			unpaid = append(unpaid, earning)
		}
	}
	sort.Slice(unpaid, func(a, b int) bool { return unpaid[a].CreatedAt.Before(unpaid[b].CreatedAt) })
	return unpaid, nil
}

func (r *InMemory) LedgerBalance(_ context.Context, account string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.balances[account], nil
}

// Apply checks everything before it changes anything, which is what one
// transaction gives Postgres: a change that conflicts anywhere writes nothing.
func (r *InMemory) Apply(_ context.Context, change domain.Change) error {
	for _, txn := range change.Txns {
		if err := txn.Validate(); err != nil {
			return err
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if payment := change.Payment; payment != nil {
		if change.From == "" {
			if _, taken := r.byTrip[payment.TripID]; taken {
				return domain.ErrConflict
			}
		} else {
			stored, ok := r.payments[payment.ID]
			if !ok {
				return domain.ErrNotFound
			}
			if stored.Status != change.From {
				return domain.ErrConflict
			}
		}
	}

	if earning := change.Earning; earning != nil {
		stored, exists := r.earnings[earning.TripID]
		switch {
		case change.EarningFrom == "" && exists:
			return domain.ErrConflict
		case change.EarningFrom != "" && !exists:
			return domain.ErrNotFound
		case change.EarningFrom != "" && stored.Status != change.EarningFrom:
			return domain.ErrConflict
		}
	}

	for _, txn := range change.Txns {
		for _, entry := range txn.Entries {
			if r.posted[txn.ID+"\x00"+entry.Account] {
				return domain.ErrConflict
			}
		}
	}

	if payment := change.Payment; payment != nil {
		r.payments[payment.ID] = *payment
		r.byTrip[payment.TripID] = payment.ID
		if payment.ProcessorPaymentID != "" {
			r.byProcessor[payment.ProcessorPaymentID] = payment.ID
		}
	}
	if earning := change.Earning; earning != nil {
		r.earnings[earning.TripID] = *earning
	}
	for _, txn := range change.Txns {
		for _, entry := range txn.Entries {
			r.posted[txn.ID+"\x00"+entry.Account] = true
			r.balances[entry.Account] += entry.AmountCents
		}
	}
	r.facts = append(r.facts, change.Facts...)
	return nil
}
