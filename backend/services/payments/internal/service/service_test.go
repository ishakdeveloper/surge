package service_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
	"github.com/ishakdeveloper/surge/services/payments/internal/infrastructure/fake"
	"github.com/ishakdeveloper/surge/services/payments/internal/infrastructure/repository"
	"github.com/ishakdeveloper/surge/services/payments/internal/service"
)

var now = time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

type rig struct {
	service   *service.Service
	repo      *repository.InMemory
	processor *fake.Processor
}

func newRig(t *testing.T) *rig {
	t.Helper()
	repo := repository.NewInMemory()
	processor := fake.New()
	ids := 0
	svc, err := service.New(service.Options{
		Repository: repo, Processor: processor, CommissionBps: 2000,
		WebURL: "https://surge.test",
		Now:    func() time.Time { return now },
		NewID:  func() string { ids++; return fmt.Sprintf("pay-%d", ids) },
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return &rig{service: svc, repo: repo, processor: processor}
}

func (r *rig) card(t *testing.T, rider, method string) {
	t.Helper()
	if err := r.repo.SaveCustomer(context.Background(), domain.Customer{
		UserID: rider, ProcessorCustomerID: "cus_" + rider, PaymentMethodID: method,
	}); err != nil {
		t.Fatal(err)
	}
}

func (r *rig) account(t *testing.T, driver, status string) {
	t.Helper()
	if err := r.repo.SavePayoutAccount(context.Background(), domain.PayoutAccount{
		DriverID: driver, ProcessorAccountID: "acct_" + driver, TransfersStatus: status,
	}); err != nil {
		t.Fatal(err)
	}
}

func (r *rig) payment(t *testing.T, tripID string) *domain.Payment {
	t.Helper()
	payment, err := r.repo.PaymentForTrip(context.Background(), tripID)
	if err != nil {
		t.Fatalf("payment for %s: %v", tripID, err)
	}
	return payment
}

func (r *rig) balance(t *testing.T, account string) int64 {
	t.Helper()
	balance, _ := r.repo.LedgerBalance(context.Background(), account)
	return balance
}

// sweepAt runs the sweeper as if the clock read `at`, with a service sharing
// this rig's repository and processor.
func (r *rig) sweepAt(t *testing.T, at time.Time) (service.SweepResult, error) {
	t.Helper()
	later, err := service.New(service.Options{
		Repository: r.repo, Processor: r.processor, CommissionBps: 2000,
		Now: func() time.Time { return at },
	})
	if err != nil {
		t.Fatal(err)
	}
	return later.Sweep(context.Background())
}

func serviceAccount(id, status string) service.ConnectedAccount {
	return service.ConnectedAccount{ID: id, TransfersStatus: status}
}

func trip(id string) service.Trip {
	return service.Trip{ID: id, RiderID: "rider-1", DriverID: "drv-1", TotalCents: 1450, Currency: "eur"}
}

func assertFacts(t *testing.T, got []domain.Fact, want ...domain.FactKind) {
	t.Helper()
	kinds := make([]domain.FactKind, len(got))
	for i, fact := range got {
		kinds[i] = fact.Kind
	}
	if fmt.Sprint(kinds) != fmt.Sprint(want) {
		t.Fatalf("facts %v, want %v", kinds, want)
	}
}

func TestARequestIsHeldOnce(t *testing.T) {
	rig := newRig(t)
	ctx := context.Background()
	rig.card(t, "rider-1", fake.CardVisa)

	for range 2 { // the second is a redelivery
		if err := rig.service.TripRequested(ctx, trip("trip-1")); err != nil {
			t.Fatalf("requested: %v", err)
		}
	}

	if payment := rig.payment(t, "trip-1"); payment.Status != domain.StatusAuthorized || payment.AmountCents != 1450 {
		t.Errorf("payment %s for %d, want authorized for 1450", payment.Status, payment.AmountCents)
	}
	if holds := rig.processor.Holds(); holds != 1 {
		t.Errorf("%d holds on the rider's card, want 1", holds)
	}
	assertFacts(t, rig.repo.Facts(), domain.FactAuthorized)
}

// No card: the payment fails with a reason the rider can be shown, and the
// processor is never troubled.
func TestNoCardFailsTheTrip(t *testing.T) {
	rig := newRig(t)
	if err := rig.service.TripRequested(context.Background(), trip("trip-1")); err != nil {
		t.Fatalf("requested: %v", err)
	}

	payment := rig.payment(t, "trip-1")
	if payment.Status != domain.StatusFailed || payment.FailureReason != domain.FailureNoPaymentMethod {
		t.Errorf("payment %s (%q), want failed for no payment method", payment.Status, payment.FailureReason)
	}
	if calls := rig.processor.Calls(); calls.Authorize != 0 {
		t.Errorf("asked the processor %d times with no card to hold", calls.Authorize)
	}
	assertFacts(t, rig.repo.Facts(), domain.FactFailed)
}

func TestADeclineFailsTheTrip(t *testing.T) {
	rig := newRig(t)
	rig.card(t, "rider-1", fake.CardDeclined)
	if err := rig.service.TripRequested(context.Background(), trip("trip-1")); err != nil {
		t.Fatalf("requested: %v", err)
	}
	if payment := rig.payment(t, "trip-1"); payment.FailureReason != domain.FailureDeclined {
		t.Errorf("failure %q, want declined", payment.FailureReason)
	}
	assertFacts(t, rig.repo.Facts(), domain.FactFailed)
}

// 3-D Secure: the trip waits on the rider, the secret is held only while it is
// needed, and a late duplicate webhook changes nothing.
func TestAuthenticationIsFinishedByTheWebhook(t *testing.T) {
	rig := newRig(t)
	ctx := context.Background()
	rig.card(t, "rider-1", fake.CardAuthenticationRequired)

	if err := rig.service.TripRequested(ctx, trip("trip-1")); err != nil {
		t.Fatalf("requested: %v", err)
	}
	waiting := rig.payment(t, "trip-1")
	if waiting.Status != domain.StatusRequiresAction || waiting.ClientSecret == "" {
		t.Fatalf("payment %s with secret %q, want requires_action with one", waiting.Status, waiting.ClientSecret)
	}

	answer := rig.processor.Authenticate(waiting.ProcessorPaymentID, true)
	for range 2 {
		if err := rig.service.AuthorizationSettled(ctx, waiting.ProcessorPaymentID, answer); err != nil {
			t.Fatalf("settled: %v", err)
		}
	}

	held := rig.payment(t, "trip-1")
	if held.Status != domain.StatusAuthorized || held.ClientSecret != "" {
		t.Errorf("payment %s with secret %q, want authorized with none", held.Status, held.ClientSecret)
	}
	assertFacts(t, rig.repo.Facts(), domain.FactActionRequired, domain.FactAuthorized)
}

// The whole point of the service: a completed ride is charged once and its
// driver paid once, however often the completion is delivered.
func TestCompletionCapturesAndPaysOnce(t *testing.T) {
	rig := newRig(t)
	ctx := context.Background()
	rig.card(t, "rider-1", fake.CardVisa)
	rig.account(t, "drv-1", domain.TransfersActive)

	if err := rig.service.TripRequested(ctx, trip("trip-1")); err != nil {
		t.Fatalf("requested: %v", err)
	}
	for range 3 {
		if err := rig.service.TripCompleted(ctx, trip("trip-1")); err != nil {
			t.Fatalf("completed: %v", err)
		}
	}

	if calls := rig.processor.Calls(); calls.Capture != 1 || calls.Transfer != 1 {
		t.Errorf("captured %d and transferred %d times, want once each", calls.Capture, calls.Transfer)
	}
	payment := rig.payment(t, "trip-1")
	if payment.Status != domain.StatusCaptured || payment.DriverID != "drv-1" || payment.CommissionCents != 290 {
		t.Errorf("payment %+v", payment)
	}

	// Where the €14.50 went: €2.90 to the platform, €11.60 to the driver, and
	// nothing left owed.
	if revenue := rig.balance(t, domain.AccountRevenue); revenue != -290 {
		t.Errorf("revenue %d, want -290", revenue)
	}
	if owed := rig.balance(t, domain.DriverAccount("drv-1")); owed != 0 {
		t.Errorf("driver still owed %d", owed)
	}
	if clearing := rig.balance(t, domain.AccountClearing); clearing != 290 {
		t.Errorf("clearing holds %d, want the commission, 290", clearing)
	}
	assertFacts(t, rig.repo.Facts(), domain.FactAuthorized, domain.FactCaptured)
}

// A driver who has not finished onboarding is owed, not skipped, and paid the
// moment their account can receive it.
func TestADriverWhoCannotReceiveIsPaidLater(t *testing.T) {
	rig := newRig(t)
	ctx := context.Background()
	rig.card(t, "rider-1", fake.CardVisa)
	rig.account(t, "drv-1", "pending")

	if err := rig.service.TripRequested(ctx, trip("trip-1")); err != nil {
		t.Fatal(err)
	}
	if err := rig.service.TripCompleted(ctx, trip("trip-1")); err != nil {
		t.Fatal(err)
	}
	if calls := rig.processor.Calls(); calls.Transfer != 0 {
		t.Fatalf("transferred to an account that cannot receive")
	}
	if owed := rig.balance(t, domain.DriverAccount("drv-1")); owed != -1160 {
		t.Fatalf("driver owed %d, want 1160 (as -1160)", owed)
	}

	rig.account(t, "drv-1", domain.TransfersActive)
	if err := rig.service.PayOutstanding(ctx, "drv-1"); err != nil {
		t.Fatalf("pay outstanding: %v", err)
	}
	if owed := rig.balance(t, domain.DriverAccount("drv-1")); owed != 0 {
		t.Errorf("driver still owed %d after their account became active", owed)
	}
}

func TestACancelReleasesTheHold(t *testing.T) {
	rig := newRig(t)
	ctx := context.Background()
	rig.card(t, "rider-1", fake.CardVisa)

	if err := rig.service.TripRequested(ctx, trip("trip-1")); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := rig.service.TripEnded(ctx, trip("trip-1")); err != nil {
			t.Fatalf("ended: %v", err)
		}
	}

	if payment := rig.payment(t, "trip-1"); payment.Status != domain.StatusReleased {
		t.Errorf("payment %s, want released", payment.Status)
	}
	if calls := rig.processor.Calls(); calls.Release != 1 {
		t.Errorf("released %d times, want once", calls.Release)
	}
	// A completion after the hold was let go has nothing to take.
	if err := rig.service.TripCompleted(ctx, trip("trip-1")); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Errorf("completing a released trip: want ErrInvalidTransition, got %v", err)
	}
}

// The processor unreachable mid-request: the payment is left authorizing,
// and the redelivery finishes it without a second hold.
func TestAnUnansweredRequestIsFinishedByTheRedelivery(t *testing.T) {
	rig := newRig(t)
	ctx := context.Background()
	rig.card(t, "rider-1", fake.CardVisa)

	rig.processor.FailNext(errors.New("connection reset"))
	if err := rig.service.TripRequested(ctx, trip("trip-1")); err == nil {
		t.Fatal("an unreachable processor should be an error the consumer retries")
	}
	if payment := rig.payment(t, "trip-1"); payment.Status != domain.StatusAuthorizing {
		t.Fatalf("payment %s, want authorizing", payment.Status)
	}

	if err := rig.service.TripRequested(ctx, trip("trip-1")); err != nil {
		t.Fatalf("redelivery: %v", err)
	}
	if payment := rig.payment(t, "trip-1"); payment.Status != domain.StatusAuthorized {
		t.Errorf("payment %s, want authorized", payment.Status)
	}
	if holds := rig.processor.Holds(); holds != 1 {
		t.Errorf("%d holds, want 1", holds)
	}
}

// A trip requested again after its hold was let go gets a new hold, under new
// keys — replaying the old key would return the released hold.
func TestARequestAfterAReleaseIsANewAttempt(t *testing.T) {
	rig := newRig(t)
	ctx := context.Background()
	rig.card(t, "rider-1", fake.CardVisa)

	if err := rig.service.TripRequested(ctx, trip("trip-1")); err != nil {
		t.Fatal(err)
	}
	first := rig.payment(t, "trip-1").ProcessorPaymentID
	if err := rig.service.TripEnded(ctx, trip("trip-1")); err != nil {
		t.Fatal(err)
	}
	if err := rig.service.TripRequested(ctx, trip("trip-1")); err != nil {
		t.Fatalf("requested again: %v", err)
	}

	again := rig.payment(t, "trip-1")
	if again.Status != domain.StatusAuthorized || again.Attempt != 2 || again.ProcessorPaymentID == first {
		t.Errorf("payment %+v, want a second authorized hold", again)
	}
	if !strings.Contains(again.IdempotencyKey("authorize"), ":2:") {
		t.Errorf("key %q does not carry the attempt", again.IdempotencyKey("authorize"))
	}
}

// A completion for a trip that was never held is refused as unheld, which the
// consumer counts and moves past, rather than charging after the fact.
func TestAnUnheldTripIsNotCharged(t *testing.T) {
	rig := newRig(t)
	if err := rig.service.TripCompleted(context.Background(), trip("trip-9")); !errors.Is(err, service.ErrUnheld) {
		t.Errorf("want ErrUnheld, got %v", err)
	}
	if calls := rig.processor.Calls(); calls.Capture != 0 {
		t.Error("captured a trip nobody was asked to pay for")
	}
}

func TestCommissionMustBeAShare(t *testing.T) {
	for _, bps := range []int{-1, 10_000} {
		if _, err := service.New(service.Options{CommissionBps: bps}); err == nil {
			t.Errorf("a commission of %d basis points was accepted", bps)
		}
	}
}
