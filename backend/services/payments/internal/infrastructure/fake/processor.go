// Package fake is a payment processor that answers deterministically, for
// tests and for running the whole system on a laptop with no Stripe account.
//
// Its payment methods are named after Stripe's test ones and behave like them,
// so a flow that passes against the fake is the same flow a Stripe test card
// takes. It keeps Stripe's idempotency too: a request repeated under the same
// key gets the first answer, which is the property every retry in the service
// leans on and the one a naive fake would quietly not have.
package fake

import (
	"context"
	"fmt"
	"sync"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
	"github.com/ishakdeveloper/surge/services/payments/internal/service"
)

// Payment methods, named and behaving like Stripe's test ones.
const (
	CardVisa                   = "pm_card_visa"
	CardAuthenticationRequired = "pm_card_threeDSecure2Required"
	// CardDeclined saves and is declined when held, as Stripe's
	// pm_card_chargeCustomerFail does. Its pm_card_chargeDeclined is refused
	// when saved, so it could never reach a booking to be declined at.
	CardDeclined = "pm_card_chargeCustomerFail"
)

type state string

const (
	stateRequiresAction state = "requires_action"
	stateAuthorized     state = "authorized"
	stateCaptured       state = "captured"
	stateReleased       state = "released"
	stateFailed         state = "failed"
)

type hold struct {
	amount int64
	state  state
}

// Calls counts what the processor was asked to do.
type Calls struct {
	Authorize, Capture, Release, Transfer int
}

type Processor struct {
	mu      sync.Mutex
	next    int
	answers map[string]any
	holds   map[string]*hold
	calls   Calls
	fail    error
}

func New() *Processor {
	return &Processor{answers: map[string]any{}, holds: map[string]*hold{}}
}

var _ service.Processor = (*Processor)(nil)

// FailNext makes the next call fail with err before it does anything: the
// processor unreachable, for exercising what the service does about it.
func (p *Processor) FailNext(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fail = err
}

// Calls returns what the processor has been asked, so far.
func (p *Processor) Calls() Calls {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// Holds is how many holds were ever placed — the number a retry must not grow.
func (p *Processor) Holds() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.holds)
}

func (p *Processor) failing() error {
	err := p.fail
	p.fail = nil
	return err
}

func (p *Processor) id(prefix string) string {
	p.next++
	return fmt.Sprintf("%s_fake_%d", prefix, p.next)
}

func (p *Processor) Authorize(_ context.Context, request service.AuthorizeRequest) (service.Authorization, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.calls.Authorize++
	if err := p.failing(); err != nil {
		return service.Authorization{}, err
	}
	if answer, ok := p.answers[request.IdempotencyKey]; ok {
		return answer.(service.Authorization), nil
	}

	id := p.id("pi")
	placed := &hold{amount: request.AmountCents}
	answer := service.Authorization{ProcessorPaymentID: id}

	switch request.PaymentMethodID {
	case CardAuthenticationRequired:
		placed.state = stateRequiresAction
		answer.Outcome = service.OutcomeActionRequired
		answer.ClientSecret = id + "_secret_fake"
	case CardDeclined:
		placed.state = stateFailed
		answer.Outcome = service.OutcomeDeclined
		answer.DeclineReason = domain.FailureDeclined
	default:
		placed.state = stateAuthorized
		answer.Outcome = service.OutcomeAuthorized
	}

	p.holds[id] = placed
	p.answers[request.IdempotencyKey] = answer
	return answer, nil
}

// Authenticate is the rider finishing — or failing — a 3-D Secure challenge.
// It returns what the processor would then report by webhook.
func (p *Processor) Authenticate(processorPaymentID string, succeeded bool) service.Authorization {
	p.mu.Lock()
	defer p.mu.Unlock()

	answer := service.Authorization{ProcessorPaymentID: processorPaymentID}
	placed := p.holds[processorPaymentID]
	if placed == nil || placed.state != stateRequiresAction {
		answer.Outcome = service.OutcomeDeclined
		answer.DeclineReason = domain.FailureAuthenticationFailed
		return answer
	}

	if succeeded {
		placed.state = stateAuthorized
		answer.Outcome = service.OutcomeAuthorized
	} else {
		placed.state = stateFailed
		answer.Outcome = service.OutcomeDeclined
		answer.DeclineReason = domain.FailureAuthenticationFailed
	}
	return answer
}

func (p *Processor) Capture(_ context.Context, request service.CaptureRequest) (service.Capture, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.calls.Capture++
	if err := p.failing(); err != nil {
		return service.Capture{}, err
	}
	if answer, ok := p.answers[request.IdempotencyKey]; ok {
		return answer.(service.Capture), nil
	}

	placed := p.holds[request.ProcessorPaymentID]
	switch {
	case placed == nil:
		return service.Capture{}, fmt.Errorf("fake: no hold %s", request.ProcessorPaymentID)
	case placed.state != stateAuthorized:
		return service.Capture{}, fmt.Errorf("fake: cannot capture %s, it is %s", request.ProcessorPaymentID, placed.state)
	case request.AmountCents > placed.amount:
		return service.Capture{}, fmt.Errorf("fake: cannot capture %d of a %d hold", request.AmountCents, placed.amount)
	}

	placed.state = stateCaptured
	answer := service.Capture{ProcessorChargeID: p.id("ch")}
	p.answers[request.IdempotencyKey] = answer
	return answer, nil
}

func (p *Processor) Release(_ context.Context, processorPaymentID, _ string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.calls.Release++
	if err := p.failing(); err != nil {
		return err
	}

	placed := p.holds[processorPaymentID]
	if placed == nil {
		return fmt.Errorf("fake: no hold %s", processorPaymentID)
	}
	switch placed.state {
	case stateAuthorized, stateRequiresAction:
		placed.state = stateReleased
	case stateCaptured:
		return fmt.Errorf("fake: cannot release %s, it was captured", processorPaymentID)
	}
	return nil
}

func (p *Processor) Transfer(_ context.Context, request service.TransferRequest) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.calls.Transfer++
	if err := p.failing(); err != nil {
		return "", err
	}
	if answer, ok := p.answers[request.IdempotencyKey]; ok {
		return answer.(string), nil
	}
	if request.DestinationAccountID == "" || request.AmountCents <= 0 {
		return "", fmt.Errorf("fake: cannot transfer %d to %q", request.AmountCents, request.DestinationAccountID)
	}

	id := p.id("tr")
	p.answers[request.IdempotencyKey] = id
	return id, nil
}

func (p *Processor) EnsureCustomer(_ context.Context, _, _, idempotencyKey string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.failing(); err != nil {
		return "", err
	}
	if answer, ok := p.answers[idempotencyKey]; ok {
		return answer.(string), nil
	}
	id := p.id("cus")
	p.answers[idempotencyKey] = id
	return id, nil
}

// CreateSetupIntent attaches a Visa on the spot. There is no browser form to
// type a card into without Stripe.js, so the fake's "saved card" is the one
// that always authorizes; the other test cards are for tests, which seed them.
func (p *Processor) CreateSetupIntent(_ context.Context, _ string) (service.SetupIntent, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.failing(); err != nil {
		return service.SetupIntent{}, err
	}
	return service.SetupIntent{
		ClientSecret: p.id("seti") + "_secret_fake",
		Attached: &service.SavedMethod{
			PaymentMethodID: CardVisa,
			Card:            domain.Card{Brand: "visa", Last4: "4242", ExpMonth: 12, ExpYear: 2030},
		},
	}, nil
}

// CreateConnectedAccount opens an account that can be paid at once: the fake
// has no identity to verify.
func (p *Processor) CreateConnectedAccount(_ context.Context, _, _, idempotencyKey string) (service.ConnectedAccount, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.failing(); err != nil {
		return service.ConnectedAccount{}, err
	}
	if answer, ok := p.answers[idempotencyKey]; ok {
		return answer.(service.ConnectedAccount), nil
	}
	account := service.ConnectedAccount{ID: p.id("acct"), TransfersStatus: domain.TransfersActive}
	p.answers[idempotencyKey] = account
	return account, nil
}

// OnboardingLink sends the driver straight back: there is nothing to fill in.
func (p *Processor) OnboardingLink(_ context.Context, _, _, returnURL string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.failing(); err != nil {
		return "", err
	}
	return returnURL, nil
}
