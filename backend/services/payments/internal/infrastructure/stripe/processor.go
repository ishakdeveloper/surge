// Package stripe is the processor that moves real money: Stripe, through
// stripe-go, as the service.Processor port describes it.
//
// Everything Stripe-shaped stops here. The service sees holds, captures and
// transfers; this is the only place that knows they are PaymentIntents with
// manual capture, Transfers funded from a charge, and v2 recipient accounts.
//
// One client, made with the key, rather than stripe-go's global key: the
// global form is deprecated, and a key that lives on a struct is one a test can
// replace and a log line cannot reach by accident.
package stripe

import (
	"context"
	"errors"
	"fmt"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
	"github.com/ishakdeveloper/surge/services/payments/internal/service"
	stripego "github.com/stripe/stripe-go/v86"
)

// Options configure the processor.
type Options struct {
	// PaymentMethodConfiguration narrows the methods a rider may save to ones
	// that can be held and captured later: cards and wallets. iDEAL and SEPA
	// cannot place a hold. Empty uses the account's default configuration.
	//
	// A configuration rather than payment_method_types, which would hard-code
	// the list here and switch off Stripe's dynamic payment methods.
	PaymentMethodConfiguration string
	// Country is where a driver's connected account is opened.
	Country string
}

type Processor struct {
	client  *stripego.Client
	options Options
}

var _ service.Processor = (*Processor)(nil)

func New(key string, options Options) *Processor {
	if options.Country == "" {
		options.Country = "nl"
	}
	return &Processor{client: stripego.NewClient(key), options: options}
}

// Client is the underlying Stripe client, for the webhook receiver.
func (p *Processor) Client() *stripego.Client { return p.client }

func (p *Processor) EnsureCustomer(ctx context.Context, userID, email, idempotencyKey string) (string, error) {
	params := &stripego.CustomerCreateParams{Email: stripego.String(email)}
	params.AddMetadata("user_id", userID)
	params.SetIdempotencyKey(idempotencyKey)

	customer, err := p.client.V1Customers.Create(ctx, params)
	if err != nil {
		return "", fmt.Errorf("stripe: create customer: %w", err)
	}
	return customer.ID, nil
}

func (p *Processor) CreateSetupIntent(ctx context.Context, customerID string) (service.SetupIntent, error) {
	params := &stripego.SetupIntentCreateParams{
		Customer: stripego.String(customerID),
		// Off session: the card is saved to be held later, at booking, which
		// is what makes the bank expect a merchant-initiated authorization
		// rather than challenging every one.
		Usage: stripego.String("off_session"),
		// No redirects, as on the hold itself. A method that redirects to be
		// saved — iDEAL, Bancontact — is saved as a SEPA mandate, and a mandate
		// cannot place a hold; the rider would save it and then fail every
		// booking. Refusing redirects here leaves cards and wallets, which are
		// what can be held. It is also what lets the browser confirm a card
		// without a return URL to come back to.
		AutomaticPaymentMethods: &stripego.SetupIntentCreateAutomaticPaymentMethodsParams{
			Enabled:        stripego.Bool(true),
			AllowRedirects: stripego.String("never"),
		},
	}
	if p.options.PaymentMethodConfiguration != "" {
		params.PaymentMethodConfiguration = stripego.String(p.options.PaymentMethodConfiguration)
	}

	intent, err := p.client.V1SetupIntents.Create(ctx, params)
	if err != nil {
		return service.SetupIntent{}, fmt.Errorf("stripe: create setup intent: %w", err)
	}
	// The card itself is reported by setup_intent.succeeded, once the rider
	// has confirmed it in the browser.
	return service.SetupIntent{ClientSecret: intent.ClientSecret}, nil
}

func (p *Processor) Authorize(ctx context.Context, request service.AuthorizeRequest) (service.Authorization, error) {
	params := &stripego.PaymentIntentCreateParams{
		Amount:        stripego.Int64(request.AmountCents),
		Currency:      stripego.String(request.Currency),
		Customer:      stripego.String(request.CustomerID),
		PaymentMethod: stripego.String(request.PaymentMethodID),
		// A hold, taken at completion.
		CaptureMethod: stripego.String("manual"),
		Confirm:       stripego.Bool(true),
		// No redirects: the rider is in the app when this runs, and a card's
		// 3-D Secure challenge runs in Stripe.js from the client secret. A
		// redirect-based method would need a return URL for a flow nothing
		// here offers.
		AutomaticPaymentMethods: &stripego.PaymentIntentCreateAutomaticPaymentMethodsParams{
			Enabled:        stripego.Bool(true),
			AllowRedirects: stripego.String("never"),
		},
		// Separate charges and transfers: the driver is not known yet, so the
		// charge is the platform's, and the transfer that pays the driver is
		// grouped with it once they are.
		TransferGroup: stripego.String(request.TripID),
	}
	params.AddMetadata("trip_id", request.TripID)
	params.AddMetadata("payment_id", request.PaymentID)
	params.SetIdempotencyKey(request.IdempotencyKey)

	intent, err := p.client.V1PaymentIntents.Create(ctx, params)
	if declined, ok := declineFrom(err); ok {
		return declined, nil
	}
	if err != nil {
		return service.Authorization{}, fmt.Errorf("stripe: create payment intent: %w", err)
	}
	return answerFor(intent)
}

// declineFrom turns a card error into an outcome. A declined card is Stripe
// answering, not Stripe failing, and retrying it would be asking the same bank
// the same question until it said something else.
func declineFrom(err error) (service.Authorization, bool) {
	var stripeErr *stripego.Error
	if !errors.As(err, &stripeErr) || stripeErr.Type != stripego.ErrorTypeCard {
		return service.Authorization{}, false
	}
	answer := service.Authorization{Outcome: service.OutcomeDeclined, DeclineReason: reasonFor(stripeErr)}
	if stripeErr.PaymentIntent != nil {
		answer.ProcessorPaymentID = stripeErr.PaymentIntent.ID
	}
	return answer, true
}

// answerFor reads what a confirmed PaymentIntent came to.
func answerFor(intent *stripego.PaymentIntent) (service.Authorization, error) {
	answer := service.Authorization{ProcessorPaymentID: intent.ID}
	switch intent.Status {
	case stripego.PaymentIntentStatusRequiresCapture:
		answer.Outcome = service.OutcomeAuthorized
	case stripego.PaymentIntentStatusRequiresAction:
		answer.Outcome = service.OutcomeActionRequired
		answer.ClientSecret = intent.ClientSecret
	case stripego.PaymentIntentStatusRequiresPaymentMethod, stripego.PaymentIntentStatusCanceled:
		answer.Outcome = service.OutcomeDeclined
		answer.DeclineReason = reasonFor(intent.LastPaymentError)
	default:
		// Processing, succeeded or unconfirmed: none of them is something a
		// card with manual capture answers at booking, and guessing would be
		// guessing about money.
		return service.Authorization{}, fmt.Errorf("stripe: payment intent %s is unexpectedly %s", intent.ID, intent.Status)
	}
	return answer, nil
}

// reasonFor maps Stripe's error to the closed set a rider is told.
func reasonFor(stripeErr *stripego.Error) string {
	if stripeErr != nil && stripeErr.Code == stripego.ErrorCode("payment_intent_authentication_failure") {
		return domain.FailureAuthenticationFailed
	}
	return domain.FailureDeclined
}

func (p *Processor) Capture(ctx context.Context, request service.CaptureRequest) (service.Capture, error) {
	params := &stripego.PaymentIntentCaptureParams{AmountToCapture: stripego.Int64(request.AmountCents)}
	params.SetIdempotencyKey(request.IdempotencyKey)

	intent, err := p.client.V1PaymentIntents.Capture(ctx, request.ProcessorPaymentID, params)
	if err != nil {
		return service.Capture{}, fmt.Errorf("stripe: capture %s: %w", request.ProcessorPaymentID, err)
	}
	if intent.LatestCharge == nil {
		return service.Capture{}, fmt.Errorf("stripe: captured %s has no charge", intent.ID)
	}
	return service.Capture{ProcessorChargeID: intent.LatestCharge.ID}, nil
}

func (p *Processor) Release(ctx context.Context, processorPaymentID, idempotencyKey string) error {
	params := &stripego.PaymentIntentCancelParams{}
	params.SetIdempotencyKey(idempotencyKey)

	_, err := p.client.V1PaymentIntents.Cancel(ctx, processorPaymentID, params)
	if err == nil {
		return nil
	}

	// Already let go — by an earlier attempt, or by Stripe expiring the hold —
	// is the outcome a release wants, not a failure to retry forever.
	var stripeErr *stripego.Error
	if errors.As(err, &stripeErr) && stripeErr.Code == stripego.ErrorCode("payment_intent_unexpected_state") {
		intent, readErr := p.client.V1PaymentIntents.Retrieve(ctx, processorPaymentID, nil)
		if readErr == nil && intent.Status == stripego.PaymentIntentStatusCanceled {
			return nil
		}
	}
	return fmt.Errorf("stripe: release %s: %w", processorPaymentID, err)
}

func (p *Processor) Transfer(ctx context.Context, request service.TransferRequest) (string, error) {
	params := &stripego.TransferCreateParams{
		Amount:      stripego.Int64(request.AmountCents),
		Currency:    stripego.String(request.Currency),
		Destination: stripego.String(request.DestinationAccountID),
		// Funded from the rider's charge, so the transfer waits for that
		// charge's money to be available instead of failing on an empty
		// platform balance, and inherits its settlement timing.
		SourceTransaction: stripego.String(request.SourceChargeID),
		TransferGroup:     stripego.String(request.TripID),
	}
	params.AddMetadata("trip_id", request.TripID)
	params.SetIdempotencyKey(request.IdempotencyKey)

	transfer, err := p.client.V1Transfers.Create(ctx, params)
	if err != nil {
		return "", fmt.Errorf("stripe: transfer for %s: %w", request.TripID, err)
	}
	return transfer.ID, nil
}

// includes asks for the parts of a v2 account this service reads. A v2
// account returns neither its configuration nor its requirements unless asked.
func includes() []*string {
	return []*string{stripego.String("configuration.recipient"), stripego.String("requirements")}
}

func (p *Processor) CreateConnectedAccount(ctx context.Context, driverID, email, idempotencyKey string) (service.ConnectedAccount, error) {
	params := &stripego.V2CoreAccountCreateParams{
		ContactEmail: stripego.String(email),
		// Express: Stripe's lightweight dashboard for the driver, and
		// Stripe-hosted onboarding, so identity documents never pass through
		// this system.
		Dashboard: stripego.String("express"),
		Identity:  &stripego.V2CoreAccountCreateIdentityParams{Country: stripego.String(p.options.Country)},
		Defaults: &stripego.V2CoreAccountCreateDefaultsParams{
			Currency: stripego.String(domain.DefaultCurrency),
			// The platform prices rides and owns disputes, so it collects the
			// fees and carries the losses — the arrangement separate charges
			// and transfers requires.
			Responsibilities: &stripego.V2CoreAccountCreateDefaultsResponsibilitiesParams{
				FeesCollector:   stripego.String("application"),
				LossesCollector: stripego.String("application"),
			},
		},
		// A recipient, not a merchant: drivers receive transfers, they do not
		// take payments, and asking for merchant capabilities only makes
		// onboarding longer.
		Configuration: &stripego.V2CoreAccountCreateConfigurationParams{
			Recipient: &stripego.V2CoreAccountCreateConfigurationRecipientParams{
				Capabilities: &stripego.V2CoreAccountCreateConfigurationRecipientCapabilitiesParams{
					StripeBalance: &stripego.V2CoreAccountCreateConfigurationRecipientCapabilitiesStripeBalanceParams{
						StripeTransfers: &stripego.V2CoreAccountCreateConfigurationRecipientCapabilitiesStripeBalanceStripeTransfersParams{
							Requested: stripego.Bool(true),
						},
					},
				},
			},
		},
		Include:  includes(),
		Metadata: map[string]string{"driver_id": driverID},
	}
	params.SetIdempotencyKey(idempotencyKey)

	account, err := p.client.V2CoreAccounts.Create(ctx, params)
	if err != nil {
		return service.ConnectedAccount{}, fmt.Errorf("stripe: create account for %s: %w", driverID, err)
	}
	if err := p.manualPayouts(ctx, account.ID, idempotencyKey+":payouts"); err != nil {
		return service.ConnectedAccount{}, err
	}
	return connected(account), nil
}

// ConnectedAccount reads an account fresh, for the thin events that only name
// one.
func (p *Processor) ConnectedAccount(ctx context.Context, accountID string) (service.ConnectedAccount, error) {
	account, err := p.client.V2CoreAccounts.Retrieve(ctx, accountID,
		&stripego.V2CoreAccountRetrieveParams{Include: includes()})
	if err != nil {
		return service.ConnectedAccount{}, fmt.Errorf("stripe: read account %s: %w", accountID, err)
	}
	return connected(account), nil
}

// connected reads whether an account can receive transfers: the capability
// status Stripe's own guidance says to check, rather than v1's deprecated
// payouts_enabled.
func connected(account *stripego.V2CoreAccount) service.ConnectedAccount {
	status := "pending"
	if config := account.Configuration; config != nil && config.Recipient != nil &&
		config.Recipient.Capabilities != nil && config.Recipient.Capabilities.StripeBalance != nil &&
		config.Recipient.Capabilities.StripeBalance.StripeTransfers != nil {
		status = string(config.Recipient.Capabilities.StripeBalance.StripeTransfers.Status)
	}
	due := account.Requirements != nil && len(account.Requirements.Entries) > 0
	return service.ConnectedAccount{ID: account.ID, TransfersStatus: status, RequirementsDue: due}
}

func (p *Processor) OnboardingLink(ctx context.Context, accountID, refreshURL, returnURL string) (string, error) {
	link, err := p.client.V2CoreAccountLinks.Create(ctx, &stripego.V2CoreAccountLinkCreateParams{
		Account: stripego.String(accountID),
		UseCase: &stripego.V2CoreAccountLinkCreateUseCaseParams{
			Type: stripego.String("account_onboarding"),
			AccountOnboarding: &stripego.V2CoreAccountLinkCreateUseCaseAccountOnboardingParams{
				Configurations: []*string{stripego.String("recipient")},
				RefreshURL:     stripego.String(refreshURL),
				ReturnURL:      stripego.String(returnURL),
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("stripe: onboarding link for %s: %w", accountID, err)
	}
	return link.URL, nil
}

// SavedCard reads the card behind a payment method, for the setup that
// saved it.
func (p *Processor) SavedCard(ctx context.Context, paymentMethodID string) (service.SavedMethod, error) {
	method, err := p.client.V1PaymentMethods.Retrieve(ctx, paymentMethodID, nil)
	if err != nil {
		return service.SavedMethod{}, fmt.Errorf("stripe: read payment method %s: %w", paymentMethodID, err)
	}
	saved := service.SavedMethod{PaymentMethodID: method.ID}
	if card := method.Card; card != nil {
		saved.Card = domain.Card{
			Brand: string(card.Brand), Last4: card.Last4,
			ExpMonth: int(card.ExpMonth), ExpYear: int(card.ExpYear),
		}
	}
	return saved, nil
}
