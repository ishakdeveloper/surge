package repository

import (
	"testing"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
	"github.com/ishakdeveloper/surge/shared/wire"
)

// The failure reasons are declared twice — in the domain, which decides them,
// and on the wire, which carries them — and must be the same strings.
func TestFailureReasonsMatchTheWire(t *testing.T) {
	pairs := [][2]string{
		{domain.FailureNoPaymentMethod, wire.FailureNoPaymentMethod},
		{domain.FailureDeclined, wire.FailureDeclined},
		{domain.FailureAuthenticationFailed, wire.FailureAuthenticationFailed},
		{domain.FailureExpired, wire.FailureExpired},
	}
	for _, pair := range pairs {
		if pair[0] != pair[1] {
			t.Errorf("the domain says %q where the wire says %q", pair[0], pair[1])
		}
	}
}

func TestEveryFactKindEncodes(t *testing.T) {
	payment := domain.Payment{ID: "pay-1", TripID: "trip-1", RiderID: "rider-1",
		AmountCents: 1450, Currency: "eur", FailureReason: domain.FailureDeclined}

	for kind := range factTags {
		messages, err := encodeFacts([]domain.Fact{{Kind: kind, Payment: payment}})
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if messages[0].Key != "trip-1" {
			t.Errorf("%s keyed %q, want the trip", kind, messages[0].Key)
		}
	}

	// A failure with no reason tells the rider nothing, and is refused.
	payment.FailureReason = ""
	if _, err := encodeFacts([]domain.Fact{{Kind: domain.FactFailed, Payment: payment}}); err == nil {
		t.Error("encoded a failure with no reason")
	}
}
