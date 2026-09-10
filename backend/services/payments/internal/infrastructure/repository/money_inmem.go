package repository

import (
	"context"
	"sort"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
)

// checkRefund holds a refund to the bounds the Postgres update enforces: one
// per key per trip, never past what was captured, never past what was earned.
// Called under the lock, before anything is changed.
func (r *InMemory) checkRefund(refund *domain.Refund) error {
	for _, existing := range r.refunds {
		if existing.TripID == refund.TripID && existing.IdempotencyKey == refund.IdempotencyKey {
			return domain.ErrConflict
		}
	}
	payment, ok := r.payments[refund.PaymentID]
	if !ok {
		return domain.ErrNotFound
	}
	if payment.Status != domain.StatusCaptured || payment.RefundedCents+refund.AmountCents > payment.CapturedCents {
		return domain.ErrConflict
	}
	if refund.TakesFromEarning() {
		earning, ok := r.earnings[refund.TripID]
		if !ok || earning.ReversedCents+refund.DriverCents > earning.NetCents {
			return domain.ErrConflict
		}
	}
	return nil
}

func (r *InMemory) applyRefund(refund *domain.Refund) {
	r.refunds[refund.ID] = *refund

	payment := r.payments[refund.PaymentID]
	payment.RefundedCents += refund.AmountCents
	if payment.RefundedCents >= payment.CapturedCents {
		payment.Status = domain.StatusRefunded
	}
	payment.UpdatedAt = refund.CreatedAt
	r.payments[refund.PaymentID] = payment

	if refund.TakesFromEarning() {
		earning := r.earnings[refund.TripID]
		earning.ReversedCents += refund.DriverCents
		if earning.ReversedCents >= earning.NetCents {
			earning.Status = domain.EarningReversed
		}
		earning.UpdatedAt = refund.CreatedAt
		r.earnings[refund.TripID] = earning
	}
}

func (r *InMemory) checkFailedRefund(failed *domain.Refund) error {
	stored, ok := r.refunds[failed.ID]
	if !ok {
		return domain.ErrNotFound
	}
	if stored.Status == domain.RefundFailed || r.payments[stored.PaymentID].RefundedCents < stored.AmountCents {
		return domain.ErrConflict
	}
	return nil
}

func (r *InMemory) applyFailedRefund(failed *domain.Refund) {
	r.refunds[failed.ID] = *failed

	payment := r.payments[failed.PaymentID]
	payment.RefundedCents -= failed.AmountCents
	if payment.Status == domain.StatusRefunded {
		payment.Status = domain.StatusCaptured
	}
	r.payments[failed.PaymentID] = payment
}

func (r *InMemory) RefundByProcessorID(_ context.Context, processorRefundID string) (*domain.Refund, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, refund := range r.refunds {
		if processorRefundID != "" && refund.ProcessorRefundID == processorRefundID {
			return &refund, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *InMemory) RefundByKey(_ context.Context, tripID, key string) (*domain.Refund, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, refund := range r.refunds {
		if refund.TripID == tripID && refund.IdempotencyKey == key {
			return &refund, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *InMemory) SaveWithdrawal(_ context.Context, withdrawal *domain.Withdrawal) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.withdrawals {
		if existing.DriverID == withdrawal.DriverID && existing.IdempotencyKey == withdrawal.IdempotencyKey {
			return domain.ErrConflict
		}
	}
	r.withdrawals[withdrawal.ID] = *withdrawal
	return nil
}

func (r *InMemory) UpdateWithdrawal(_ context.Context, withdrawal *domain.Withdrawal, from domain.WithdrawalStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored, ok := r.withdrawals[withdrawal.ID]
	if !ok {
		return domain.ErrNotFound
	}
	if stored.Status != from {
		return domain.ErrConflict
	}
	r.withdrawals[withdrawal.ID] = *withdrawal
	return nil
}

func (r *InMemory) WithdrawalByKey(_ context.Context, driverID, key string) (*domain.Withdrawal, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, withdrawal := range r.withdrawals {
		if withdrawal.DriverID == driverID && withdrawal.IdempotencyKey == key {
			return &withdrawal, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *InMemory) WithdrawalByPayoutID(_ context.Context, processorPayoutID string) (*domain.Withdrawal, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, withdrawal := range r.withdrawals {
		if processorPayoutID != "" && withdrawal.ProcessorPayoutID == processorPayoutID {
			return &withdrawal, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *InMemory) ListWithdrawals(_ context.Context, filter domain.WithdrawalFilter) (domain.WithdrawalPage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var found []domain.Withdrawal
	for _, withdrawal := range r.withdrawals {
		if withdrawal.DriverID == filter.DriverID {
			found = append(found, withdrawal)
		}
	}
	sort.Slice(found, func(a, b int) bool {
		if !found[a].CreatedAt.Equal(found[b].CreatedAt) {
			return found[a].CreatedAt.After(found[b].CreatedAt)
		}
		return found[a].ID > found[b].ID
	})
	if filter.Cursor != "" {
		for i, withdrawal := range found {
			if withdrawal.ID == filter.Cursor {
				found = found[i+1:]
				break
			}
		}
	}

	page := domain.WithdrawalPage{}
	if len(found) > filter.Limit {
		found = found[:filter.Limit]
		page.NextCursor = found[len(found)-1].ID
	}
	page.Withdrawals = found
	return page, nil
}
