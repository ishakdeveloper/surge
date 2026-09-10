package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
)

// Balance is what a connected account holds, as the processor reports it.
type Balance struct {
	// AvailableCents can be paid out now.
	AvailableCents int64
	// PendingCents has arrived and has not yet settled.
	PendingCents int64
}

type PayoutRequest struct {
	AccountID      string
	AmountCents    int64
	Currency       string
	IdempotencyKey string
}

type Payout struct {
	ID string
	// Status is the processor's word: pending, in_transit, paid, failed or
	// canceled.
	Status string
}

var (
	// ErrNoPayoutAccount: there is nowhere to pay out from yet.
	ErrNoPayoutAccount   = errors.New("service: set up payouts before withdrawing")
	ErrNothingToWithdraw = errors.New("service: nothing is available to withdraw")
	ErrOverBalance       = errors.New("service: that is more than is available")
	// ErrPayoutRefused is the processor declining a payout for a reason the
	// driver can act on — no bank account yet, most often. The reason follows
	// the colon, and is shown to them.
	ErrPayoutRefused = errors.New("service: the payout was refused")
)

// DriverBalance is what a driver sees: what they can take out now, what is on
// its way, and what they have earned that has not reached them yet.
type DriverBalance struct {
	AvailableCents int64
	PendingCents   int64
	// OwedCents is earned and not yet transferred: waiting on onboarding.
	OwedCents   int64
	Currency    string
	CanWithdraw bool
}

// Balance reads a driver's balance. The processor is the source of truth for
// what their account holds — it is their money, in their account — and this
// service only adds what it still owes them.
func (s *Service) Balance(ctx context.Context, driverID string) (DriverBalance, error) {
	totals, err := s.repo.EarningTotals(ctx, driverID)
	if err != nil {
		return DriverBalance{}, err
	}
	balance := DriverBalance{OwedCents: totals.OwedCents, Currency: domain.DefaultCurrency}

	account, found, err := s.PayoutAccount(ctx, driverID)
	if err != nil || !found {
		return balance, err
	}
	held, err := s.processor.Balance(ctx, account.ProcessorAccountID)
	if err != nil {
		return DriverBalance{}, err
	}
	balance.AvailableCents, balance.PendingCents = held.AvailableCents, held.PendingCents
	balance.CanWithdraw = account.CanReceive() && held.AvailableCents > 0
	return balance, nil
}

// Withdraw pays out from a driver's balance to their bank. An amount of zero
// withdraws everything available.
//
// Safe to retry: the key names the withdrawal, and a withdrawal that was asked
// for and never answered is asked for again under the same processor key, so
// the double tap and the retried request are one payout.
func (s *Service) Withdraw(ctx context.Context, driverID string, amountCents int64, key string) (*domain.Withdrawal, error) {
	if key == "" {
		return nil, ErrIdempotencyKeyRequired
	}

	withdrawal, err := s.repo.WithdrawalByKey(ctx, driverID, key)
	switch {
	case err == nil:
		if withdrawal.Status != domain.WithdrawalRequested || withdrawal.ProcessorPayoutID != "" {
			return withdrawal, nil
		}
	case errors.Is(err, domain.ErrNotFound):
		if withdrawal, err = s.newWithdrawal(ctx, driverID, amountCents, key); err != nil {
			return nil, err
		}
	default:
		return nil, err
	}
	return s.payOut(ctx, withdrawal)
}

func (s *Service) newWithdrawal(ctx context.Context, driverID string, amountCents int64, key string) (*domain.Withdrawal, error) {
	account, found, err := s.PayoutAccount(ctx, driverID)
	if err != nil {
		return nil, err
	}
	if !found || !account.CanReceive() {
		return nil, ErrNoPayoutAccount
	}

	held, err := s.processor.Balance(ctx, account.ProcessorAccountID)
	if err != nil {
		return nil, err
	}
	if amountCents == 0 {
		amountCents = held.AvailableCents
	}
	switch {
	case amountCents <= 0:
		return nil, ErrNothingToWithdraw
	case amountCents > held.AvailableCents:
		return nil, fmt.Errorf("%w: %d asked, %d available", ErrOverBalance, amountCents, held.AvailableCents)
	}

	now := s.now()
	withdrawal := &domain.Withdrawal{
		ID: s.newID(), DriverID: driverID, AmountCents: amountCents, Currency: domain.DefaultCurrency,
		Status: domain.WithdrawalRequested, IdempotencyKey: key, CreatedAt: now, UpdatedAt: now,
	}
	// Stored before the processor is asked, as a hold is: a crash in between
	// leaves a withdrawal the retry finishes.
	err = s.repo.SaveWithdrawal(ctx, withdrawal)
	if errors.Is(err, domain.ErrConflict) {
		return s.repo.WithdrawalByKey(ctx, driverID, key)
	}
	if err != nil {
		return nil, err
	}
	return withdrawal, nil
}

func (s *Service) payOut(ctx context.Context, withdrawal *domain.Withdrawal) (*domain.Withdrawal, error) {
	account, err := s.repo.PayoutAccount(ctx, withdrawal.DriverID)
	if err != nil {
		return nil, err
	}

	payout, payoutErr := s.processor.Payout(ctx, PayoutRequest{
		AccountID: account.ProcessorAccountID, AmountCents: withdrawal.AmountCents,
		Currency: withdrawal.Currency, IdempotencyKey: "withdrawal:" + withdrawal.ID,
	})

	next := *withdrawal
	next.UpdatedAt = s.now()
	switch {
	case errors.Is(payoutErr, ErrPayoutRefused):
		next.Status, next.FailureReason = domain.WithdrawalFailed, refusal(payoutErr)
	case payoutErr != nil:
		return nil, fmt.Errorf("service: pay out %s: %w", withdrawal.ID, payoutErr)
	default:
		next.ProcessorPayoutID, next.Status = payout.ID, withdrawalStatusOf(payout.Status)
	}

	if err := s.repo.UpdateWithdrawal(ctx, &next, domain.WithdrawalRequested); err != nil {
		return nil, err
	}
	if next.Status == domain.WithdrawalFailed {
		return &next, payoutErr
	}
	return &next, nil
}

// PayoutSettled applies what the processor finally reported about a payout.
func (s *Service) PayoutSettled(ctx context.Context, processorPayoutID, status, reason string) error {
	withdrawal, err := s.repo.WithdrawalByPayoutID(ctx, processorPayoutID)
	if err != nil {
		return err
	}

	to := withdrawalStatusOf(status)
	// A failure is final. Paid is not quite — a bank can return a payout
	// days later, and Stripe then reports it failed — so paid may still move.
	if withdrawal.Status == to || withdrawal.Status == domain.WithdrawalFailed {
		return nil
	}

	next := *withdrawal
	next.Status, next.UpdatedAt = to, s.now()
	if to == domain.WithdrawalFailed {
		next.FailureReason = reason
	}
	if err := s.repo.UpdateWithdrawal(ctx, &next, withdrawal.Status); err != nil {
		return err
	}
	s.tell(ctx, "", withdrawal.DriverID)
	return nil
}

// Withdrawals is a page of a driver's withdrawals, newest first.
func (s *Service) Withdrawals(ctx context.Context, driverID string, limit int, cursor string) (domain.WithdrawalPage, error) {
	switch {
	case limit <= 0:
		limit = DefaultPageSize
	case limit > MaxPageSize:
		limit = MaxPageSize
	}
	return s.repo.ListWithdrawals(ctx, domain.WithdrawalFilter{DriverID: driverID, Limit: limit, Cursor: cursor})
}

func withdrawalStatusOf(processorStatus string) domain.WithdrawalStatus {
	switch processorStatus {
	case "paid":
		return domain.WithdrawalPaid
	case "failed", "canceled":
		return domain.WithdrawalFailed
	default:
		return domain.WithdrawalInTransit
	}
}

// refusal is the processor's reason from an ErrPayoutRefused.
func refusal(err error) string {
	return strings.TrimPrefix(err.Error(), ErrPayoutRefused.Error()+": ")
}
