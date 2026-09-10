package service_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
	"github.com/ishakdeveloper/surge/services/payments/internal/infrastructure/fake"
)

// A rider's first card creates their customer; the next one reuses it.
func TestSavingACardCreatesOneCustomer(t *testing.T) {
	rig := newRig(t)
	ctx := context.Background()

	before, err := rig.service.Customer(ctx, "rider-1")
	if err != nil || before.CanPay() {
		t.Fatalf("a new rider: %+v (%v), want no card and no error", before, err)
	}

	for range 2 {
		if secret, err := rig.service.SetupCard(ctx, "rider-1", "rider@example.com"); err != nil || secret == "" {
			t.Fatalf("setup: %q (%v)", secret, err)
		}
	}

	customer, err := rig.service.Customer(ctx, "rider-1")
	if err != nil {
		t.Fatalf("customer: %v", err)
	}
	if !customer.CanPay() || customer.Card.Last4 != "4242" {
		t.Errorf("customer %+v, want the fake's Visa saved", customer)
	}
	if customer.ProcessorCustomerID != "cus_fake_1" {
		t.Errorf("customer %s, want the first one reused", customer.ProcessorCustomerID)
	}

	// And the card it saved is the one the next hold is placed on.
	if err := rig.service.TripRequested(ctx, trip("trip-1")); err != nil {
		t.Fatal(err)
	}
	if payment := rig.payment(t, "trip-1"); payment.Status != domain.StatusAuthorized {
		t.Errorf("a hold on the saved card is %s", payment.Status)
	}
}

// A driver who drove before onboarding is paid when they finish it.
func TestOnboardingPaysWhatIsOwed(t *testing.T) {
	rig := newRig(t)
	ctx := context.Background()
	rig.card(t, "rider-1", fake.CardVisa)

	if err := rig.service.TripRequested(ctx, trip("trip-1")); err != nil {
		t.Fatal(err)
	}
	if err := rig.service.TripCompleted(ctx, trip("trip-1")); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := rig.service.PayoutAccount(ctx, "drv-1"); found {
		t.Fatal("a driver who never onboarded has an account")
	}

	link, err := rig.service.StartOnboarding(ctx, "drv-1", "driver@example.com")
	if err != nil {
		t.Fatalf("onboarding: %v", err)
	}
	if link != "https://surge.test/drive/payouts/return" {
		t.Errorf("link %q, want the server's own return address", link)
	}

	_, totals, err := rig.service.Earnings(ctx, "drv-1", 0, "")
	if err != nil {
		t.Fatalf("earnings: %v", err)
	}
	if totals.OwedCents != 0 || totals.PaidCents != 1160 {
		t.Errorf("owed %d, paid %d; want 0 and 1160", totals.OwedCents, totals.PaidCents)
	}

	// Onboarding again does not open a second account.
	first, _, _ := rig.service.PayoutAccount(ctx, "drv-1")
	if _, err := rig.service.StartOnboarding(ctx, "drv-1", "driver@example.com"); err != nil {
		t.Fatalf("onboarding again: %v", err)
	}
	again, _, _ := rig.service.PayoutAccount(ctx, "drv-1")
	if again.ProcessorAccountID != first.ProcessorAccountID {
		t.Errorf("account %s became %s; onboarding twice opened another",
			first.ProcessorAccountID, again.ProcessorAccountID)
	}
}

// An account the processor reports active is paid everything outstanding.
func TestAnAccountUpdateSweepsWhatIsOwed(t *testing.T) {
	rig := newRig(t)
	ctx := context.Background()
	rig.card(t, "rider-1", fake.CardVisa)
	rig.account(t, "drv-1", "pending")

	for _, id := range []string{"trip-1", "trip-2"} {
		if err := rig.service.TripRequested(ctx, trip(id)); err != nil {
			t.Fatal(err)
		}
		if err := rig.service.TripCompleted(ctx, trip(id)); err != nil {
			t.Fatal(err)
		}
	}

	if err := rig.service.AccountUpdated(ctx, serviceAccount("acct_drv-1", domain.TransfersActive)); err != nil {
		t.Fatalf("account updated: %v", err)
	}
	if calls := rig.processor.Calls(); calls.Transfer != 2 {
		t.Errorf("transferred %d times, want once per trip", calls.Transfer)
	}
}

func TestEarningsArePaged(t *testing.T) {
	rig := newRig(t)
	ctx := context.Background()
	for i := range 5 {
		earning := domain.Earning{
			TripID: fmt.Sprintf("trip-%d", i), DriverID: "drv-1", NetCents: 100,
			Status: domain.EarningUnpaid, CreatedAt: now.Add(time.Duration(i) * time.Minute),
		}
		if err := rig.repo.Apply(ctx, domain.Change{Earning: &earning}); err != nil {
			t.Fatal(err)
		}
	}

	first, totals, err := rig.service.Earnings(ctx, "drv-1", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Earnings) != 2 || first.Earnings[0].TripID != "trip-4" || first.NextCursor == "" {
		t.Fatalf("first page %+v", first)
	}
	if totals.OwedCents != 500 {
		t.Errorf("owed %d across all pages, want 500", totals.OwedCents)
	}

	second, _, _ := rig.service.Earnings(ctx, "drv-1", 2, first.NextCursor)
	if len(second.Earnings) != 2 || second.Earnings[0].TripID != "trip-2" {
		t.Errorf("second page %+v", second)
	}
}
