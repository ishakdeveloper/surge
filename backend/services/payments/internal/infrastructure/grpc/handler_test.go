package grpc

import (
	"testing"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
	paymentspb "github.com/ishakdeveloper/surge/shared/proto/payments"
)

// A status missing from the table goes out as UNSPECIFIED, which a client
// cannot render and would read as "the server forgot". Every one is mapped.
func TestEveryStatusIsMapped(t *testing.T) {
	for _, status := range []domain.Status{
		domain.StatusAuthorizing, domain.StatusRequiresAction, domain.StatusAuthorized,
		domain.StatusCaptured, domain.StatusReleased, domain.StatusFailed, domain.StatusRefunded,
	} {
		if paymentStatuses[status] == paymentspb.PaymentStatus_PAYMENT_STATUS_UNSPECIFIED {
			t.Errorf("payment status %s is not mapped", status)
		}
	}
	for _, status := range []domain.EarningStatus{
		domain.EarningUnpaid, domain.EarningTransferred, domain.EarningReversed,
	} {
		if earningStatuses[status] == paymentspb.EarningStatus_EARNING_STATUS_UNSPECIFIED {
			t.Errorf("earning status %s is not mapped", status)
		}
	}
}

// The secret that confirms a payment is the rider's alone.
func TestOnlyTheOwnerSeesTheClientSecret(t *testing.T) {
	payment := &domain.Payment{Status: domain.StatusRequiresAction, ClientSecret: "pi_1_secret"}

	if got := toPayment(payment, true).GetClientSecret(); got != "pi_1_secret" {
		t.Errorf("the rider sees %q", got)
	}
	if got := toPayment(payment, false).GetClientSecret(); got != "" {
		t.Errorf("someone else sees %q", got)
	}
}
