package stripe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
	"github.com/ishakdeveloper/surge/services/payments/internal/service"
	stripego "github.com/stripe/stripe-go/v86"
)

// EventLog remembers which webhooks have been applied.
type EventLog interface {
	EventHandled(ctx context.Context, eventID string) (bool, error)
	MarkEventHandled(ctx context.Context, eventID, eventType string) error
}

// Webhooks verifies Stripe's webhooks and applies them to payments.
//
// Webhooks are how Stripe says what happened after the fact: a rider finishing
// 3-D Secure, a card saved in the browser, a driver's account becoming able to
// receive money. None of them is trusted until its signature is checked, and
// each is applied once however often it is delivered.
type Webhooks struct {
	processor  *Processor
	payments   *service.Service
	events     EventLog
	secret     string
	thinSecret string
}

func NewWebhooks(processor *Processor, payments *service.Service, events EventLog, secret, thinSecret string) *Webhooks {
	return &Webhooks{processor: processor, payments: payments, events: events, secret: secret, thinSecret: thinSecret}
}

// Receive verifies and applies one webhook. A signature that does not check
// out is service.ErrInvalidWebhook; any other error is worth Stripe retrying.
func (w *Webhooks) Receive(ctx context.Context, thin bool, payload []byte, signature string) error {
	if thin {
		return w.receiveThin(ctx, payload, signature)
	}

	// The API version is not checked against the SDK's. Stripe signs events
	// in the account's version, `stripe listen` in whatever it was set to,
	// and the handful of fields read below are the same in all of them.
	event, err := stripego.ConstructEvent(payload, signature, w.secret, stripego.WithIgnoreAPIVersionMismatch())
	if err != nil {
		return fmt.Errorf("%w: %v", service.ErrInvalidWebhook, err)
	}
	return w.once(ctx, event.ID, string(event.Type), func() error { return w.apply(ctx, event) })
}

// once applies an event unless it already has been, and records it after.
func (w *Webhooks) once(ctx context.Context, eventID, eventType string, apply func() error) error {
	handled, err := w.events.EventHandled(ctx, eventID)
	if err != nil || handled {
		return err
	}
	if err := acknowledgeable(apply()); err != nil {
		return err
	}
	return w.events.MarkEventHandled(ctx, eventID, eventType)
}

// acknowledgeable lets an event about something this service never recorded
// — a payment from another integration on the same account, a move the state
// machine already made — be acknowledged, rather than retried by Stripe for
// three days to the same answer.
func acknowledgeable(err error) error {
	if errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrInvalidTransition) {
		return nil
	}
	return err
}

func (w *Webhooks) apply(ctx context.Context, event stripego.Event) error {
	switch event.Type {
	case stripego.EventTypePaymentIntentAmountCapturableUpdated:
		// The hold is in place: the rider finished authenticating, or the
		// synchronous answer was lost. Either way, the trip may go.
		var intent stripego.PaymentIntent
		if err := json.Unmarshal(event.Data.Raw, &intent); err != nil {
			return fmt.Errorf("stripe: decode %s: %w", event.Type, err)
		}
		return w.payments.AuthorizationSettled(ctx, intent.ID, service.Authorization{
			ProcessorPaymentID: intent.ID, Outcome: service.OutcomeAuthorized,
		})

	case stripego.EventTypePaymentIntentPaymentFailed:
		var intent stripego.PaymentIntent
		if err := json.Unmarshal(event.Data.Raw, &intent); err != nil {
			return fmt.Errorf("stripe: decode %s: %w", event.Type, err)
		}
		return w.payments.AuthorizationSettled(ctx, intent.ID, service.Authorization{
			ProcessorPaymentID: intent.ID, Outcome: service.OutcomeDeclined,
			DeclineReason: reasonFor(intent.LastPaymentError),
		})

	case stripego.EventTypeSetupIntentSucceeded:
		var intent stripego.SetupIntent
		if err := json.Unmarshal(event.Data.Raw, &intent); err != nil {
			return fmt.Errorf("stripe: decode %s: %w", event.Type, err)
		}
		if intent.Customer == nil || intent.PaymentMethod == nil {
			return nil
		}
		saved, err := w.processor.SavedCard(ctx, intent.PaymentMethod.ID)
		if err != nil {
			return err
		}
		return w.payments.CardSaved(ctx, intent.Customer.ID, saved)

	default:
		// Captures, releases and transfers are driven from this side and
		// stored as they are made; their events confirm what is known.
		return nil
	}
}

// receiveThin handles an event destination's notifications, which name an
// object rather than carry it. For accounts that is the point: the account is
// read fresh, so two notifications arriving out of order cannot leave an older
// status stored over a newer one.
func (w *Webhooks) receiveThin(ctx context.Context, payload []byte, signature string) error {
	notification, err := w.processor.Client().ParseEventNotification(payload, signature, w.thinSecret)
	if err != nil {
		return fmt.Errorf("%w: %v", service.ErrInvalidWebhook, err)
	}

	var accountID string
	switch n := notification.(type) {
	case *stripego.V2CoreAccountIncludingConfigurationRecipientCapabilityStatusUpdatedEventNotification:
		accountID = n.RelatedObject.ID
	case *stripego.V2CoreAccountIncludingRequirementsUpdatedEventNotification:
		accountID = n.RelatedObject.ID
	default:
		return nil
	}

	base := notification.GetEventNotification()
	return w.once(ctx, base.ID, base.Type, func() error {
		account, err := w.processor.ConnectedAccount(ctx, accountID)
		if err != nil {
			return err
		}
		return w.payments.AccountUpdated(ctx, account)
	})
}
