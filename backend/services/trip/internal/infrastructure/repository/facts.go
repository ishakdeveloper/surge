package repository

import (
	"fmt"

	"github.com/ishakdeveloper/surge/services/trip/internal/domain"
	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/ishakdeveloper/surge/shared/outbox"
	"github.com/ishakdeveloper/surge/shared/wire"
)

// factTags is the one place a domain fact becomes a wire tag.
var factTags = map[domain.FactKind]string{
	domain.FactRequested: wire.FactTripRequested,
	domain.FactAccepted:  wire.FactTripAccepted,
	domain.FactCompleted: wire.FactTripCompleted,
	domain.FactCancelled: wire.FactTripCancelled,
	domain.FactUnmatched: wire.FactTripUnmatched,
}

// encodeFacts turns facts into outbox messages on `trip.lifecycle`, keyed by
// trip id so every fact about one trip lands on one partition, in order.
//
// Encoded before the transaction opens, so a fact that cannot be expressed
// fails the write rather than committing a change nobody will hear about.
func encodeFacts(facts []domain.Fact) ([]outbox.Message, error) {
	messages := make([]outbox.Message, 0, len(facts))
	for _, fact := range facts {
		tag, ok := factTags[fact.Kind]
		if !ok {
			return nil, fmt.Errorf("repository: fact %q has no wire tag", fact.Kind)
		}

		trip := fact.Trip
		value := wire.TripFact{
			Tag:        tag,
			TripID:     trip.ID,
			RiderID:    trip.RiderID,
			DriverID:   trip.DriverID,
			TotalCents: trip.TotalCents,
			Currency:   trip.Currency,
			Reason:     trip.CancelReason,
			AtMs:       trip.UpdatedAt.UnixMilli(),
		}
		if !value.Valid() {
			return nil, fmt.Errorf("repository: %s for trip %s is not a valid fact", tag, trip.ID)
		}

		messages = append(messages, outbox.Message{
			Topic: kafkax.TopicTripLifecycle,
			Key:   trip.ID,
			Value: value,
		})
	}
	return messages, nil
}
