package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
)

type RefundRequest struct {
	ProcessorPaymentID string
	AmountCents        int64
	Reason             string
	TripID             string
	IdempotencyKey     string
}

type ReversalRequest struct {
	TransferID     string
	AmountCents    int64
	IdempotencyKey string
}

var (
	// ErrNotRefundable is a payment that was never captured, or has been given
	// back in full already.
	ErrNotRefundable = errors.New("service: only a captured payment can be refunded")
	// ErrRefundTooLarge is more than is left to give back.
	ErrRefundTooLarge = errors.New("service: that is more than is left to refund")
	// ErrReversalRefused is the processor declining to take a driver's share
	// back, because their balance no longer holds it.
	ErrReversalRefused = errors.New("service: the driver's balance cannot cover the reversal")
	// ErrIdempotencyKeyRequired: a request that moves money must be safe to
	// retry, and only a key makes it so.
	ErrIdempotencyKeyRequired = errors.New("service: an idempotency key is required")
)

// Refund gives money back to a rider, and takes the driver's share of it back
// in proportion. An amount of zero refunds everything left.
//
// Safe to retry: the key names the refund here and at the processor, so the
// retried click returns the refund the first one made.
func (s *Service) Refund(ctx context.Context, tripID string, amountCents int64, reason, key string) (*domain.Refund, error) {
	if key == "" {
		return nil, ErrIdempotencyKeyRequired
	}
	if existing, err := s.repo.RefundByKey(ctx, tripID, key); err == nil {
		return existing, nil
	} else if !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}

	payment, err := s.repo.PaymentForTrip(ctx, tripID)
	if err != nil {
		return nil, err
	}
	if payment.Status != domain.StatusCaptured {
		return nil, fmt.Errorf("%w: the payment is %s", ErrNotRefundable, payment.Status)
	}
	left := payment.CapturedCents - payment.RefundedCents
	if amountCents == 0 {
		amountCents = left
	}
	if amountCents <= 0 || amountCents > left {
		return nil, fmt.Errorf("%w: %d asked, %d left", ErrRefundTooLarge, amountCents, left)
	}

	refund := &domain.Refund{
		ID: s.newID(), TripID: tripID, PaymentID: payment.ID,
		AmountCents: amountCents, Currency: payment.Currency, Reason: reason,
		Reversal: domain.ReversalNone, Status: domain.RefundSucceeded, IdempotencyKey: key, CreatedAt: s.now(),
	}
	processorKey := domain.RefundKey(tripID, key)

	refund.ProcessorRefundID, err = s.processor.Refund(ctx, RefundRequest{
		ProcessorPaymentID: payment.ProcessorPaymentID, AmountCents: amountCents,
		Reason: reason, TripID: tripID, IdempotencyKey: processorKey,
	})
	if err != nil {
		return nil, fmt.Errorf("service: refund trip %s: %w", tripID, err)
	}

	earning, err := s.repo.Earning(ctx, tripID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}

	driverID := ""
	if earning != nil {
		driverID = earning.DriverID
		refund.DriverCents = domain.DriverShareOf(*earning, amountCents)
		if err := s.takeBack(ctx, refund, earning, processorKey); err != nil {
			return nil, err
		}
	}

	txns := []domain.Txn{domain.RefundTxn(*refund, driverID)}
	if refund.Reversal == domain.ReversalReversed {
		txns = append(txns, domain.ReversalTxn(*refund, driverID))
	}

	err = s.apply(ctx, domain.Change{Refund: refund, Txns: txns})
	if errors.Is(err, domain.ErrConflict) {
		// A retry with the same key landed first. Both reached the processor
		// under the same key, so there is one refund; return it.
		return s.repo.RefundByKey(ctx, tripID, key)
	}
	if err != nil {
		return nil, err
	}
	return refund, nil
}

// takeBack decides how the driver's share of a refund comes back.
func (s *Service) takeBack(ctx context.Context, refund *domain.Refund, earning *domain.Earning, processorKey string) error {
	switch {
	case refund.DriverCents == 0:
		refund.Reversal = domain.ReversalNone
	case earning.Status == domain.EarningUnpaid:
		// Not paid out yet: nothing to fetch back, only less to pay.
		refund.Reversal = domain.ReversalDeducted
	case earning.Status == domain.EarningTransferred:
		id, err := s.processor.ReverseTransfer(ctx, ReversalRequest{
			TransferID: earning.ProcessorTransferID, AmountCents: refund.DriverCents,
			IdempotencyKey: processorKey + ":reversal",
		})
		switch {
		case errors.Is(err, ErrReversalRefused):
			// Withdrawn already. The rider still gets their money back; the
			// platform is left owed, and the ledger says so.
			refund.Reversal = domain.ReversalFailed
		case err != nil:
			return fmt.Errorf("service: reverse transfer for %s: %w", refund.TripID, err)
		default:
			refund.Reversal, refund.ProcessorReversalID = domain.ReversalReversed, id
		}
	default:
		refund.DriverCents, refund.Reversal = 0, domain.ReversalNone
	}
	return nil
}
