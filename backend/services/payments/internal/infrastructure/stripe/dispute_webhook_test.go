package stripe_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
	"github.com/ishakdeveloper/surge/services/payments/internal/infrastructure/fake"
	"github.com/ishakdeveloper/surge/services/payments/internal/infrastructure/repository"
	surgestripe "github.com/ishakdeveloper/surge/services/payments/internal/infrastructure/stripe"
	"github.com/ishakdeveloper/surge/services/payments/internal/service"
)

// A dispute opens and is won, as Stripe reports it; an inquiry moves nothing.
func TestDisputeWebhooks(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewInMemory()
	processor := fake.New()
	payments, _ := service.New(service.Options{Repository: repo, Processor: processor, CommissionBps: 2000})

	if err := repo.SaveCustomer(ctx, domain.Customer{UserID: "rider-1", ProcessorCustomerID: "cus_1", PaymentMethodID: fake.CardVisa}); err != nil {
		t.Fatal(err)
	}
	trip := service.Trip{ID: "trip-1", RiderID: "rider-1", DriverID: "drv-1", TotalCents: 1450, Currency: "eur"}
	if err := payments.TripRequested(ctx, trip); err != nil {
		t.Fatal(err)
	}
	if err := payments.TripCompleted(ctx, trip); err != nil {
		t.Fatal(err)
	}
	captured, _ := repo.PaymentForTrip(ctx, "trip-1")

	webhooks := surgestripe.NewWebhooks(nil, payments, repo, secret, secret)
	deliver := func(eventID, eventType, status, disputeID string) {
		t.Helper()
		payload, signature := signed(t, eventID, eventType, fmt.Sprintf(
			`{"id":%q,"object":"dispute","amount":1450,"currency":"eur","status":%q,"reason":"fraudulent","payment_intent":%q}`,
			disputeID, status, captured.ProcessorPaymentID))
		if err := webhooks.Receive(ctx, false, payload, signature); err != nil {
			t.Fatalf("%s: %v", eventType, err)
		}
	}

	deliver("evt_inquiry", "charge.dispute.created", "warning_needs_response", "dp_inquiry")
	if _, err := repo.DisputeByProcessorID(ctx, "dp_inquiry"); err == nil {
		t.Error("an inquiry was recorded as a dispute")
	}

	deliver("evt_open", "charge.dispute.created", "needs_response", "dp_1")
	opened, err := repo.DisputeByProcessorID(ctx, "dp_1")
	if err != nil || opened.Status != domain.DisputeOpen || opened.DriverCents != 1160 {
		t.Fatalf("dispute %+v (%v)", opened, err)
	}

	deliver("evt_won", "charge.dispute.closed", "won", "dp_1")
	if won, _ := repo.DisputeByProcessorID(ctx, "dp_1"); won.Status != domain.DisputeWon {
		t.Errorf("dispute %s after winning", won.Status)
	}
}
