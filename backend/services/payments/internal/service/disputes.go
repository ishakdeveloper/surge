package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
)

// DisputeOpened is what the processor reports when a rider's bank disputes a
// charge.
type DisputeOpened struct {
	ProcessorDisputeID string
	ProcessorPaymentID string
	AmountCents        int64
	Reason             string
}

// DisputeOpened takes the driver's share of a disputed charge back while the
// dispute is open. The platform has already lost the money to the bank; the
// driver's part of it comes back the way a refund's would.
func (s *Service) DisputeOpened(ctx context.Context, opened DisputeOpened) error {
	if _, err := s.repo.DisputeByProcessorID(ctx, opened.ProcessorDisputeID); err == nil {
		return nil
	} else if !errors.Is(err, domain.ErrNotFound) {
		return err
	}

	payment, err := s.repo.PaymentByProcessorID(ctx, opened.ProcessorPaymentID)
	if err != nil {
		return err
	}
	earning, err := s.repo.Earning(ctx, payment.TripID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}

	now := s.now()
	dispute := &domain.Dispute{
		ID: s.newID(), ProcessorDisputeID: opened.ProcessorDisputeID,
		TripID: payment.TripID, PaymentID: payment.ID,
		AmountCents: min(opened.AmountCents, payment.CapturedCents), Currency: payment.Currency,
		Reason: opened.Reason, Reversal: domain.ReversalNone, Status: domain.DisputeOpen,
		CreatedAt: now, UpdatedAt: now,
	}

	change := domain.Change{Dispute: dispute}
	driverID := ""
	if earning != nil {
		driverID = earning.DriverID
		dispute.DriverCents = domain.DriverShareOf(*earning, dispute.AmountCents)
		next, err := s.takeBackForDispute(ctx, dispute, earning)
		if err != nil {
			return err
		}
		if next != nil {
			change.Earning, change.EarningFrom = next, earning.Status
		}
	}

	change.Txns = []domain.Txn{domain.DisputeTxn(*dispute, driverID)}
	if dispute.Reversal == domain.ReversalReversed {
		change.Txns = append(change.Txns, domain.DisputeReversalTxn(*dispute, driverID))
	}

	err = s.repo.Apply(ctx, change)
	if errors.Is(err, domain.ErrConflict) {
		// The same dispute, delivered twice at once. The reversal went out
		// under one key, so there is one; the other delivery stored it.
		return nil
	}
	return err
}

// takeBackForDispute takes the driver's share back, and returns the earning
// as it should now be stored — or nil when it does not change.
func (s *Service) takeBackForDispute(ctx context.Context, dispute *domain.Dispute, earning *domain.Earning) (*domain.Earning, error) {
	switch {
	case dispute.DriverCents == 0:
		return nil, nil
	case earning.Status == domain.EarningUnpaid:
		dispute.Reversal = domain.ReversalDeducted
	case earning.Status == domain.EarningTransferred:
		id, err := s.processor.ReverseTransfer(ctx, ReversalRequest{
			TransferID: earning.ProcessorTransferID, AmountCents: dispute.DriverCents,
			IdempotencyKey: "dispute:" + dispute.ProcessorDisputeID + ":reversal",
		})
		switch {
		case errors.Is(err, ErrReversalRefused):
			dispute.Reversal = domain.ReversalFailed
			return nil, nil
		case err != nil:
			return nil, fmt.Errorf("service: reverse transfer for dispute %s: %w", dispute.ProcessorDisputeID, err)
		}
		dispute.Reversal, dispute.ProcessorReversalID = domain.ReversalReversed, id
	default:
		dispute.DriverCents = 0
		return nil, nil
	}

	next := *earning
	next.ReversedCents += dispute.DriverCents
	if next.ReversedCents >= next.NetCents {
		next.Status = domain.EarningReversed
	}
	next.UpdatedAt = s.now()
	return &next, nil
}

// DisputeClosed settles a dispute. Won, the money is back and so is the
// driver's share; lost, it stays gone.
func (s *Service) DisputeClosed(ctx context.Context, processorDisputeID string, won bool) error {
	dispute, err := s.repo.DisputeByProcessorID(ctx, processorDisputeID)
	if err != nil {
		return err
	}
	if dispute.Status != domain.DisputeOpen {
		return nil
	}

	next := *dispute
	next.UpdatedAt = s.now()
	if !won {
		next.Status = domain.DisputeLost
		return s.repo.Apply(ctx, domain.Change{Dispute: &next, DisputeFrom: domain.DisputeOpen})
	}
	next.Status = domain.DisputeWon

	earning, err := s.repo.Earning(ctx, dispute.TripID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	driverID := ""
	if earning != nil {
		driverID = earning.DriverID
	}

	change := domain.Change{
		Dispute: &next, DisputeFrom: domain.DisputeOpen,
		Txns: []domain.Txn{domain.DisputeWonTxn(next, driverID)},
	}

	// Owed again if it was never paid out, sent again if it was; and if the
	// driver kept it all along because the reversal failed, the won
	// transaction alone clears what they owed.
	back, err := s.giveBack(ctx, dispute.TripID, dispute.Reversal, dispute.DriverCents, dispute.Currency,
		"dispute:"+dispute.ProcessorDisputeID+":restore")
	if err != nil {
		return err
	}
	change.Earning, change.EarningFrom = back.earning, back.from
	if back.transferID != "" {
		next.ProcessorRestoreTransferID = back.transferID
		change.Txns = append(change.Txns, domain.DisputeRestoreTxn(next, driverID))
	}

	if err := s.repo.Apply(ctx, change); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return nil
		}
		return err
	}
	if back.owed {
		return s.payDriver(ctx, dispute.TripID)
	}
	return nil
}

// restore gives a dispute's share back to an earning. One that the dispute
// had reversed entirely returns to the status it had before.
func restore(earning domain.Earning, cents int64, before domain.EarningStatus, s *Service) domain.Earning {
	earning.ReversedCents -= cents
	if earning.Status == domain.EarningReversed {
		earning.Status = before
	}
	earning.UpdatedAt = s.now()
	return earning
}
