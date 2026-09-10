package events

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/ishakdeveloper/surge/services/trip/internal/domain"
	"github.com/ishakdeveloper/surge/services/trip/internal/infrastructure/wirestatus"
	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/ishakdeveloper/surge/shared/tracing"
	"github.com/ishakdeveloper/surge/shared/wire"
	"github.com/twmb/franz-go/pkg/kgo"
)

// TripNotifier tells the people on a trip that it moved.
//
// A push to `ws.push`, keyed by user id, which every gateway instance consumes
// and delivers to whichever socket that user holds. This is what replaces a
// rider's client polling for "has anybody accepted yet" — the answer arrives
// the moment the trip service records it.
//
// Best effort, deliberately. The trip is already committed when this runs, and
// a push that fails to reach a rider who has since closed the tab must not
// fail the transition that preceded it. The REST read is the source of truth;
// this is the doorbell.
type TripNotifier struct{ producer *kgo.Client }

func NewTripNotifier(producer *kgo.Client) *TripNotifier {
	return &TripNotifier{producer: producer}
}

func (n *TripNotifier) TripChanged(ctx context.Context, trip *domain.Trip) {
	payload, err := json.Marshal(wire.ServerMessage{Tag: wire.TagTripUpdated, Trip: &wire.TripUpdate{
		TripID:   trip.ID,
		RiderID:  trip.RiderID,
		DriverID: trip.DriverID,
		Status:   wirestatus.Of(trip.Status).String(),
		AtMs:     trip.UpdatedAt.UnixMilli(),
	}})
	if err != nil {
		slog.Error("trip update could not be encoded", "trip", trip.ID, "error", err)
		return
	}

	recipients := []string{trip.RiderID}
	if trip.DriverID != "" {
		recipients = append(recipients, trip.DriverID)
	}

	// Detached from the caller's lifetime for the same reason a match request
	// is: this runs inside a gRPC handler or a consumer loop, and a push
	// abandoned because the request that caused it has returned is a rider who
	// never learns their driver arrived.
	produceCtx := context.WithoutCancel(ctx)

	for _, recipient := range recipients {
		tracing.Produce(produceCtx, n.producer, &kgo.Record{
			Topic: kafkax.TopicWSPush,
			Key:   []byte(recipient),
			Value: payload,
		}, func(_ *kgo.Record, err error) {
			if err != nil {
				slog.Warn("trip update never reached the broker",
					"trip", trip.ID, "recipient", recipient, "error", err)
			}
		})
	}
}
