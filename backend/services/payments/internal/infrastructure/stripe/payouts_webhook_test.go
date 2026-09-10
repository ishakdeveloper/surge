package stripe_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
	"github.com/ishakdeveloper/surge/services/payments/internal/infrastructure/fake"
	"github.com/ishakdeveloper/surge/services/payments/internal/infrastructure/repository"
	surgestripe "github.com/ishakdeveloper/surge/services/payments/internal/infrastructure/stripe"
	"github.com/ishakdeveloper/surge/services/payments/internal/service"
)

// A withdrawal reaching the bank is reported by a Connect webhook from the
// driver's account, and settles the withdrawal it names.
func TestAPaidPayoutSettlesItsWithdrawal(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewInMemory()
	payments, _ := service.New(service.Options{Repository: repo, Processor: fake.New(), CommissionBps: 2000})

	now := time.Now()
	withdrawal := &domain.Withdrawal{ID: "wd-1", DriverID: "drv-1", AmountCents: 1160, Currency: "eur",
		Status: domain.WithdrawalInTransit, ProcessorPayoutID: "po_1", IdempotencyKey: "tap-1",
		CreatedAt: now, UpdatedAt: now}
	if err := repo.SaveWithdrawal(ctx, withdrawal); err != nil {
		t.Fatal(err)
	}

	webhooks := surgestripe.NewWebhooks(nil, payments, repo, secret, secret)
	payload, signature := signed(t, "evt_payout", "payout.paid",
		fmt.Sprintf(`{"id":%q,"object":"payout","status":"paid","amount":1160,"currency":"eur"}`, "po_1"))
	if err := webhooks.Receive(ctx, false, payload, signature); err != nil {
		t.Fatalf("receive: %v", err)
	}

	settled, _ := repo.WithdrawalByPayoutID(ctx, "po_1")
	if settled.Status != domain.WithdrawalPaid {
		t.Errorf("withdrawal %s, want paid", settled.Status)
	}
}
