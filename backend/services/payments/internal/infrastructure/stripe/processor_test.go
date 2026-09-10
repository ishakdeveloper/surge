package stripe_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
	surgestripe "github.com/ishakdeveloper/surge/services/payments/internal/infrastructure/stripe"
	"github.com/ishakdeveloper/surge/services/payments/internal/service"
	stripego "github.com/stripe/stripe-go/v86"
)

// The contract, against Stripe's test mode.
//
// The fake processor is what the service is tested against; this is what
// holds the fake honest. It runs only with a test key — never a live one — and
// skips otherwise, so `go test ./...` stays useful with no Stripe account.
func processor(t *testing.T) (*surgestripe.Processor, *stripego.Client) {
	t.Helper()
	key := os.Getenv("STRIPE_SECRET_KEY")
	if key == "" {
		t.Skip("STRIPE_SECRET_KEY is not set")
	}
	if !strings.HasPrefix(key, "sk_test_") && !strings.HasPrefix(key, "rk_test_") {
		t.Skip("STRIPE_SECRET_KEY is not a test-mode key; the contract test never runs against live")
	}
	return surgestripe.New(key, surgestripe.Options{}), stripego.NewClient(key)
}

// run keys every request uniquely, so a rerun is a new request rather than a
// replay of the last run's answers.
func run(label string) string { return label + "-" + time.Now().Format("150405.000000") }

// customerWith opens a customer with one of Stripe's test cards attached, as a
// rider who saved it in the browser would have.
func customerWith(t *testing.T, p *surgestripe.Processor, raw *stripego.Client, card string) (string, string) {
	t.Helper()
	ctx := context.Background()
	customer, err := p.EnsureCustomer(ctx, "contract-rider", "contract@example.com", run("customer"))
	if err != nil {
		t.Fatalf("customer: %v", err)
	}
	method, err := raw.V1PaymentMethods.Attach(ctx, card, &stripego.PaymentMethodAttachParams{Customer: stripego.String(customer)})
	if err != nil {
		t.Fatalf("attach %s: %v", card, err)
	}
	return customer, method.ID
}

func authorize(t *testing.T, p *surgestripe.Processor, customer, method string) service.Authorization {
	t.Helper()
	answer, err := p.Authorize(context.Background(), service.AuthorizeRequest{
		CustomerID: customer, PaymentMethodID: method, AmountCents: 1450, Currency: "eur",
		TripID: run("trip"), PaymentID: run("payment"), IdempotencyKey: run("authorize"),
	})
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	return answer
}

func TestHoldCaptureAndRelease(t *testing.T) {
	p, raw := processor(t)
	ctx := context.Background()
	customer, visa := customerWith(t, p, raw, "pm_card_visa")

	held := authorize(t, p, customer, visa)
	if held.Outcome != service.OutcomeAuthorized {
		t.Fatalf("a Visa was %s, want authorized", held.Outcome)
	}

	captured, err := p.Capture(ctx, service.CaptureRequest{
		ProcessorPaymentID: held.ProcessorPaymentID, AmountCents: 1450, IdempotencyKey: run("capture"),
	})
	if err != nil || !strings.HasPrefix(captured.ProcessorChargeID, "ch_") && !strings.HasPrefix(captured.ProcessorChargeID, "py_") {
		t.Fatalf("capture: %+v (%v)", captured, err)
	}

	released := authorize(t, p, customer, visa)
	key := run("release")
	for range 2 { // a release repeated is still a release
		if err := p.Release(ctx, released.ProcessorPaymentID, key); err != nil {
			t.Fatalf("release: %v", err)
		}
	}
	if err := p.Release(ctx, released.ProcessorPaymentID, run("release-again")); err != nil {
		t.Errorf("releasing an already-released hold under a new key: %v", err)
	}
}

// A decline is an answer, not an error to retry.
//
// pm_card_chargeCustomerFail rather than pm_card_chargeDeclined: Stripe
// declines the latter when it is attached, so a rider could never have saved
// it. This one saves and is declined when held — the case that actually
// reaches a booking.
func TestADeclineIsAnOutcome(t *testing.T) {
	p, raw := processor(t)
	customer, declined := customerWith(t, p, raw, "pm_card_chargeCustomerFail")

	answer := authorize(t, p, customer, declined)
	if answer.Outcome != service.OutcomeDeclined || answer.DeclineReason != domain.FailureDeclined {
		t.Errorf("a declined card came back %+v", answer)
	}
}

// A card that needs 3-D Secure waits on the rider, with a secret for Stripe.js.
func TestAuthenticationWaitsOnTheRider(t *testing.T) {
	p, raw := processor(t)
	customer, card := customerWith(t, p, raw, "pm_card_authenticationRequired")

	answer := authorize(t, p, customer, card)
	if answer.Outcome != service.OutcomeActionRequired || answer.ClientSecret == "" {
		t.Errorf("an authentication-required card came back %+v", answer)
	}
}

// A card saved the way the browser saves one: the setup intent confirms with
// a card and no return URL. It once could not — it accepted redirect-based
// methods, and Stripe refused to confirm it without somewhere to redirect back
// to — and only a confirm against Stripe shows that.
func TestASetupIntentConfirmsWithACard(t *testing.T) {
	p, raw := processor(t)
	ctx := context.Background()
	customer, err := p.EnsureCustomer(ctx, "contract-rider", "contract@example.com", run("customer"))
	if err != nil {
		t.Fatal(err)
	}
	intent, err := p.CreateSetupIntent(ctx, customer)
	if err != nil {
		t.Fatalf("setup intent: %v", err)
	}
	id, _, found := strings.Cut(intent.ClientSecret, "_secret_")
	if !found {
		t.Fatalf("client secret %q does not name its setup intent", intent.ClientSecret)
	}

	confirmed, err := raw.V1SetupIntents.Confirm(ctx, id, &stripego.SetupIntentConfirmParams{
		PaymentMethod: stripego.String("pm_card_visa"),
	})
	if err != nil {
		t.Fatalf("confirming with a card: %v", err)
	}
	if confirmed.Status != stripego.SetupIntentStatusSucceeded || confirmed.PaymentMethod == nil {
		t.Fatalf("confirmed as %s", confirmed.Status)
	}

	saved, err := p.SavedCard(ctx, confirmed.PaymentMethod.ID)
	if err != nil || saved.Card.Last4 != "4242" || saved.Card.Brand != "visa" {
		t.Errorf("the saved card reads %+v (%v)", saved, err)
	}
}

// A new driver's account exists but cannot be paid until they onboard, and
// the link into onboarding is Stripe's.
func TestAConnectedAccountStartsUnpaid(t *testing.T) {
	p, _ := processor(t)
	ctx := context.Background()

	account, err := p.CreateConnectedAccount(ctx, "contract-driver", "driver@example.com", run("account"))
	if err != nil {
		t.Fatalf("account: %v", err)
	}
	if !strings.HasPrefix(account.ID, "acct_") || account.TransfersStatus == domain.TransfersActive {
		t.Errorf("a fresh account came back %+v", account)
	}

	link, err := p.OnboardingLink(ctx, account.ID, "https://example.com/refresh", "https://example.com/return")
	if err != nil || !strings.HasPrefix(link, "https://connect.stripe.com/") {
		t.Errorf("onboarding link %q (%v)", link, err)
	}

	again, err := p.ConnectedAccount(ctx, account.ID)
	if err != nil || again.ID != account.ID {
		t.Errorf("reading the account back: %+v (%v)", again, err)
	}
}
