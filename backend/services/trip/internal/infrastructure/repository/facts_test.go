package repository

import (
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/trip/internal/domain"
	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/ishakdeveloper/surge/shared/wire"
)

// Every fact the domain can make has a wire form a consumer accepts. A kind
// added to the domain without a tag fails here, not as a trip that completed
// in silence.
func TestEveryFactKindEncodes(t *testing.T) {
	trip := domain.Trip{
		ID: "trip-1", RiderID: "rider-1", DriverID: "drv-1",
		TotalCents: 1450, Currency: domain.MarketCurrency,
		UpdatedAt: time.UnixMilli(1757512331000),
	}

	for _, kind := range []domain.FactKind{
		domain.FactRequested, domain.FactAccepted, domain.FactCompleted, domain.FactCancelled, domain.FactUnmatched,
	} {
		messages, err := encodeFacts([]domain.Fact{{Kind: kind, Trip: trip}})
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		message := messages[0]
		if message.Topic != kafkax.TopicTripLifecycle || message.Key != trip.ID {
			t.Errorf("%s: published to %s keyed %q", kind, message.Topic, message.Key)
		}
		if fact := message.Value.(wire.TripFact); fact.Tag != factTags[kind] || fact.AtMs != 1757512331000 {
			t.Errorf("%s: encoded as %+v", kind, fact)
		}
	}
}

// A completion without a driver has nobody to pay, and is refused before the
// transaction opens rather than committed and discovered by payments.
func TestAnUnpayableCompletionIsRefused(t *testing.T) {
	trip := domain.Trip{ID: "trip-1", RiderID: "rider-1"}
	if _, err := encodeFacts([]domain.Fact{{Kind: domain.FactCompleted, Trip: trip}}); err == nil {
		t.Fatal("encoded a completed trip with no driver")
	}
}

// An acceptance without a driver would open a conversation with nobody.
func TestADriverlessAcceptanceIsRefused(t *testing.T) {
	trip := domain.Trip{ID: "trip-1", RiderID: "rider-1"}
	if _, err := encodeFacts([]domain.Fact{{Kind: domain.FactAccepted, Trip: trip}}); err == nil {
		t.Fatal("encoded an accepted trip with no driver")
	}
}
