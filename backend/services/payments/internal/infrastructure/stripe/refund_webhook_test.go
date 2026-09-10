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

func TestARefundFailedWebhookUndoesTheRefund(t *testing.T) {
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
	refund, err := payments.Refund(ctx, "trip-1", 0, "requested_by_customer", "ops-1")
	if err != nil {
		t.Fatal(err)
	}

	processor.FailRefund(refund.ProcessorRefundID)
	webhooks := surgestripe.NewWebhooks(nil, payments, repo, secret, secret)
	payload, signature := signed(t, "evt_refund_failed", "refund.failed", fmt.Sprintf(
		`{"id":%q,"object":"refund","amount":1450,"currency":"eur","status":"failed","failure_reason":"expired_or_canceled_card"}`,
		refund.ProcessorRefundID))
	if err := webhooks.Receive(ctx, false, payload, signature); err != nil {
		t.Fatalf("receive: %v", err)
	}

	stored, _ := repo.RefundByKey(ctx, "trip-1", "ops-1")
	if stored.Status != domain.RefundFailed || stored.FailureReason != "expired_or_canceled_card" {
		t.Errorf("refund %+v, want failed with Stripe's reason", stored)
	}
	if payment, _ := repo.PaymentForTrip(ctx, "trip-1"); payment.Status != domain.StatusCaptured {
		t.Errorf("payment %s, want captured again", payment.Status)
	}
}
