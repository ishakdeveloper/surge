package repository

import (
	"context"
	"sort"
	"time"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
)

// checkDispute holds a dispute to what the Postgres writes enforce: one per
// processor dispute, and a compare-and-set on its status. Under the lock.
func (r *InMemory) checkDispute(dispute *domain.Dispute, from domain.DisputeStatus) error {
	if from == "" {
		for _, existing := range r.disputes {
			if existing.ProcessorDisputeID == dispute.ProcessorDisputeID {
				return domain.ErrConflict
			}
		}
		return nil
	}
	stored, ok := r.disputes[dispute.ID]
	if !ok {
		return domain.ErrNotFound
	}
	if stored.Status != from {
		return domain.ErrConflict
	}
	return nil
}

func (r *InMemory) DisputeByProcessorID(_ context.Context, processorDisputeID string) (*domain.Dispute, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, dispute := range r.disputes {
		if dispute.ProcessorDisputeID == processorDisputeID {
			return &dispute, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *InMemory) StalePayments(_ context.Context, status domain.Status, before time.Time, limit int) ([]domain.Payment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var stale []domain.Payment
	for _, payment := range r.payments {
		if payment.Status == status && payment.UpdatedAt.Before(before) {
			stale = append(stale, payment)
		}
	}
	sort.Slice(stale, func(a, b int) bool { return stale[a].UpdatedAt.Before(stale[b].UpdatedAt) })
	if len(stale) > limit {
		stale = stale[:limit]
	}
	return stale, nil
}

func (r *InMemory) OwedDrivers(_ context.Context, limit int) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	seen := map[string]bool{}
	var drivers []string
	for _, earning := range r.earnings {
		account, ok := r.accounts[earning.DriverID]
		if earning.Status != domain.EarningUnpaid || !ok || !account.CanReceive() || seen[earning.DriverID] {
			continue
		}
		seen[earning.DriverID] = true
		drivers = append(drivers, earning.DriverID)
	}
	sort.Strings(drivers)
	if len(drivers) > limit {
		drivers = drivers[:limit]
	}
	return drivers, nil
}
