package stripe_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
	"github.com/ishakdeveloper/surge/services/payments/internal/infrastructure/fake"
	"github.com/ishakdeveloper/surge/services/payments/internal/infrastructure/repository"
	surgestripe "github.com/ishakdeveloper/surge/services/payments/internal/infrastructure/stripe"
	"github.com/ishakdeveloper/surge/services/payments/internal/service"
	stripego "github.com/stripe/stripe-go/v86"
)

const secret = "whsec_test_secret"

// signed builds a webhook the way Stripe sends one, signed with the secret.
func signed(t *testing.T, eventID, eventType, object string) ([]byte, string) {
	t.Helper()
	payload := fmt.Sprintf(`{"id":%q,"object":"event","type":%q,"api_version":%q,"data":{"object":%s}}`,
		eventID, eventType, stripego.APIVersion, object)
	signedPayload := stripego.GenerateTestSignedPayload(&stripego.UnsignedPayload{
		Payload: []byte(payload), Secret: secret, Timestamp: time.Now(),
	})
	return signedPayload.Payload, signedPayload.Header
}

// A rider finishing 3-D Secure is reported by webhook, and the trip may go —
// once, however often Stripe delivers it.
func TestAnAuthenticatedHoldSettlesOnce(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewInMemory()
	payments, err := service.New(service.Options{Repository: repo, Processor: fake.New(), CommissionBps: 2000})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveCustomer(ctx, domain.Customer{UserID: "rider-1", ProcessorCustomerID: "cus_1",
		PaymentMethodID: fake.CardAuthenticationRequired}); err != nil {
		t.Fatal(err)
	}
	if err := payments.TripRequested(ctx, service.Trip{ID: "trip-1", RiderID: "rider-1", TotalCents: 1450, Currency: "eur"}); err != nil {
		t.Fatal(err)
	}
	waiting, _ := repo.PaymentForTrip(ctx, "trip-1")

	webhooks := surgestripe.NewWebhooks(nil, payments, repo, secret, secret)
	payload, signature := signed(t, "evt_1", "payment_intent.amount_capturable_updated",
		fmt.Sprintf(`{"id":%q,"object":"payment_intent","status":"requires_capture"}`, waiting.ProcessorPaymentID))

	for range 2 {
		if err := webhooks.Receive(ctx, false, payload, signature); err != nil {
			t.Fatalf("receive: %v", err)
		}
	}

	held, _ := repo.PaymentForTrip(ctx, "trip-1")
	if held.Status != domain.StatusAuthorized {
		t.Errorf("payment %s, want authorized", held.Status)
	}
	if facts := repo.Facts(); len(facts) != 2 || facts[1].Kind != domain.FactAuthorized {
		t.Errorf("facts %+v, want action required then authorized", facts)
	}
}

// Anything not signed with the secret is refused before it is read.
func TestAnUnsignedWebhookIsRefused(t *testing.T) {
	webhooks := surgestripe.NewWebhooks(nil, nil, repository.NewInMemory(), secret, secret)
	payload, _ := signed(t, "evt_2", "payment_intent.payment_failed", `{"id":"pi_1","object":"payment_intent"}`)

	err := webhooks.Receive(context.Background(), false, payload, "t=1,v1=forged")
	if !errors.Is(err, service.ErrInvalidWebhook) {
		t.Errorf("a forged signature: want ErrInvalidWebhook, got %v", err)
	}
}

// An event about a payment this service never made is acknowledged, not
// retried by Stripe for days to the same answer.
func TestAnEventForAnUnknownPaymentIsAcknowledged(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewInMemory()
	payments, _ := service.New(service.Options{Repository: repo, Processor: fake.New(), CommissionBps: 2000})
	webhooks := surgestripe.NewWebhooks(nil, payments, repo, secret, secret)

	payload, signature := signed(t, "evt_3", "payment_intent.payment_failed", `{"id":"pi_elsewhere","object":"payment_intent"}`)
	if err := webhooks.Receive(ctx, false, payload, signature); err != nil {
		t.Errorf("an unknown payment: %v", err)
	}
	if handled, _ := repo.EventHandled(ctx, "evt_3"); !handled {
		t.Error("the acknowledged event was not recorded")
	}
}
