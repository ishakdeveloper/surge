package grpc

import (
	"testing"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
	paymentspb "github.com/ishakdeveloper/surge/shared/proto/payments"
)

func TestEveryWithdrawalAndReversalIsMapped(t *testing.T) {
	for _, status := range []domain.WithdrawalStatus{
		domain.WithdrawalRequested, domain.WithdrawalInTransit, domain.WithdrawalPaid, domain.WithdrawalFailed,
	} {
		if withdrawalStatuses[status] == paymentspb.WithdrawalStatus_WITHDRAWAL_STATUS_UNSPECIFIED {
			t.Errorf("withdrawal status %s is not mapped", status)
		}
	}
	for _, reversal := range []domain.ReversalKind{
		domain.ReversalNone, domain.ReversalDeducted, domain.ReversalReversed, domain.ReversalFailed,
	} {
		if refundReversals[reversal] == paymentspb.RefundReversal_REFUND_REVERSAL_UNSPECIFIED {
			t.Errorf("refund reversal %s is not mapped", reversal)
		}
	}
}
