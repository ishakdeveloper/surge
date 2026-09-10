package repository

import (
	"fmt"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/ishakdeveloper/surge/shared/outbox"
	"github.com/ishakdeveloper/surge/shared/wire"
)

// factTags is the one place a domain fact becomes a wire tag.
var factTags = map[domain.FactKind]string{
	domain.FactAuthorized:     wire.FactPaymentAuthorized,
	domain.FactActionRequired: wire.FactPaymentActionRequired,
	domain.FactFailed:         wire.FactPaymentFailed,
	domain.FactCaptured:       wire.FactPaymentCaptured,
}

// encodeFacts turns facts into outbox messages on `payment.events`, keyed by
// trip id, so the trip service reads a trip's payment facts in order.
func encodeFacts(facts []domain.Fact) ([]outbox.Message, error) {
	messages := make([]outbox.Message, 0, len(facts))
	for _, fact := range facts {
		tag, ok := factTags[fact.Kind]
		if !ok {
			return nil, fmt.Errorf("repository: fact %q has no wire tag", fact.Kind)
		}

		payment := fact.Payment
		value := wire.PaymentFact{
			Tag:         tag,
			TripID:      payment.TripID,
			RiderID:     payment.RiderID,
			PaymentID:   payment.ID,
			AmountCents: payment.AmountCents,
			Currency:    payment.Currency,
			Reason:      payment.FailureReason,
			AtMs:        payment.UpdatedAt.UnixMilli(),
		}
		if !value.Valid() {
			return nil, fmt.Errorf("repository: %s for trip %s is not a valid fact", tag, payment.TripID)
		}

		messages = append(messages, outbox.Message{
			Topic: kafkax.TopicPaymentEvents,
			Key:   payment.TripID,
			Value: value,
		})
	}
	return messages, nil
}
