package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
)

// restoration is a driver's share given back: the earning as it should now be
// stored, and the transfer that sent it again, if one had to.
type restoration struct {
	earning    *domain.Earning
	from       domain.EarningStatus
	transferID string
	// owed is a share given back as owed rather than sent: pay it when the
	// driver's account can take it.
	owed bool
}

// giveBack returns a driver's share that was taken back, once the money it was
// taken back for has come back to the platform after all — a dispute won, a
// refund that never reached the rider.
//
// Decided by where the earning is now, not by how the share was taken. A share
// deducted while the earning was unpaid may since have been paid out with the
// rest, and then giving it back means sending it, not owing it.
func (s *Service) giveBack(ctx context.Context, tripID string, reversal domain.ReversalKind, cents int64, currency, key string) (restoration, error) {
	if cents == 0 || (reversal != domain.ReversalDeducted && reversal != domain.ReversalReversed) {
		// Nothing was taken, or the reversal failed and the driver kept it
		// all along: there is nothing to give.
		return restoration{}, nil
	}

	earning, err := s.repo.Earning(ctx, tripID)
	if err != nil {
		return restoration{}, err
	}

	neverSent := earning.Status == domain.EarningUnpaid ||
		(earning.Status == domain.EarningReversed && reversal == domain.ReversalDeducted)
	if neverSent {
		restored := restore(*earning, cents, domain.EarningUnpaid, s)
		return restoration{earning: &restored, from: earning.Status, owed: true}, nil
	}

	account, err := s.repo.PayoutAccount(ctx, earning.DriverID)
	if err != nil {
		return restoration{}, err
	}
	payment, err := s.repo.PaymentForTrip(ctx, tripID)
	if err != nil {
		return restoration{}, err
	}
	transferID, err := s.processor.Transfer(ctx, TransferRequest{
		DestinationAccountID: account.ProcessorAccountID, AmountCents: cents, Currency: currency,
		SourceChargeID: payment.ProcessorChargeID, TripID: tripID, IdempotencyKey: key,
	})
	if err != nil {
		return restoration{}, fmt.Errorf("service: give back %d to the driver of %s: %w", cents, tripID, err)
	}
	restored := restore(*earning, cents, domain.EarningTransferred, s)
	return restoration{earning: &restored, from: earning.Status, transferID: transferID}, nil
}

// RefundFailed undoes a refund the processor could not deliver.
//
// The rider was never repaid, so everything the refund took is put back: its
// amount on the payment, which is refundable again, and the driver's share of
// it, which was theirs for a ride that happened. What is left is a person to
// repay by other means, which is why it is logged: this is ops' to act on.
func (s *Service) RefundFailed(ctx context.Context, processorRefundID, reason string) error {
	refund, err := s.repo.RefundByProcessorID(ctx, processorRefundID)
	if err != nil {
		return err
	}
	if refund.Status == domain.RefundFailed {
		return nil
	}

	driverID := ""
	if earning, err := s.repo.Earning(ctx, refund.TripID); err == nil {
		driverID = earning.DriverID
	} else if !errors.Is(err, domain.ErrNotFound) {
		return err
	}

	next := *refund
	next.Status, next.FailureReason = domain.RefundFailed, reason

	back, err := s.giveBack(ctx, refund.TripID, refund.Reversal, refund.DriverCents, refund.Currency,
		domain.RefundKey(refund.TripID, refund.IdempotencyKey)+":restore")
	if err != nil {
		return err
	}
	next.ProcessorRestoreTransferID = back.transferID

	change := domain.Change{
		FailedRefund: &next, Earning: back.earning, EarningFrom: back.from,
		Txns: []domain.Txn{domain.RefundFailedTxn(next, driverID)},
	}
	if back.transferID != "" {
		change.Txns = append(change.Txns, domain.RefundRestoreTxn(next, driverID))
	}

	if err := s.apply(ctx, change); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return nil
		}
		return err
	}

	slog.Warn("a refund could not reach the rider; they must be repaid another way",
		"trip", refund.TripID, "refund", refund.ID, "amount_cents", refund.AmountCents, "reason", reason)
	if back.owed {
		return s.payDriver(ctx, refund.TripID)
	}
	return nil
}
