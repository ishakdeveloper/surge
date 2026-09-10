// Package events connects the trip service to the message bus.
//
// Both directions: it asks the matcher for a driver, and it listens for what
// the matcher decided. Neither is a gRPC call, deliberately — matching takes as
// long as a human takes to answer an offer, and a rider must not hold an open
// RPC for eight seconds to discover it.
package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/ishakdeveloper/surge/internal/trip/domain"
	"github.com/ishakdeveloper/surge/pkg/geo"
	"github.com/ishakdeveloper/surge/pkg/kafkax"
	"github.com/ishakdeveloper/surge/pkg/tracing"
	"github.com/ishakdeveloper/surge/pkg/wire"
	"github.com/twmb/franz-go/pkg/kgo"
)

// MatchRequester publishes a trip to the matching engine.
type MatchRequester struct{ producer *kgo.Client }

func NewMatchRequester(producer *kgo.Client) *MatchRequester {
	return &MatchRequester{producer: producer}
}

func (m *MatchRequester) RequestMatch(ctx context.Context, trip *domain.Trip) error {
	// Keyed by the pickup's resolution-7 cell, which is what routes the request
	// to the one matcher instance that owns that piece of the city.
	cell, err := geo.ShardCell(trip.Pickup)
	if err != nil {
		return fmt.Errorf("events: pickup is not a valid location: %w", err)
	}

	event := wire.GeoEvent{
		Tag:  wire.TagMatchRequested,
		Cell: cell.String(),
		AtMs: trip.CreatedAt.UnixMilli(),
		Requested: &wire.MatchRequestPayload{
			TripID:        trip.ID,
			RiderID:       trip.RiderID,
			PickupLat:     trip.Pickup.Lat,
			PickupLng:     trip.Pickup.Lng,
			DropLat:       trip.Dropoff.Lat,
			DropLng:       trip.Dropoff.Lng,
			RequestedAtMs: trip.CreatedAt.UnixMilli(),
			// The trip id is already unique per booking and the booking was
			// already made idempotent by its own key, so this is the right
			// value rather than a second one to keep in step.
			IdempotencyKey: trip.ID,
		},
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("events: encode match request: %w", err)
	}

	// WithoutCancel, and this is not a detail.
	//
	// `ctx` here is the gRPC request context, and it is cancelled the instant
	// the handler returns its response. Producing against it means the record
	// is aborted before it reaches the broker roughly every time — the booking
	// succeeds, the rider is told a trip exists, and no matcher ever hears
	// about it. WithoutCancel keeps the trace context, so the produce still
	// joins the caller's span, while detaching the lifetime.
	//
	// The callback is not nil for the same reason: a produce failure with no
	// callback is a trip that silently never gets matched.
	produceCtx := context.WithoutCancel(ctx)

	tracing.Produce(produceCtx, m.producer, &kgo.Record{
		Topic: kafkax.TopicGeoEvents,
		Key:   []byte(cell.String()),
		Value: payload,
	}, func(_ *kgo.Record, err error) {
		if err != nil {
			slog.Error("match request never reached the broker",
				"trip", trip.ID, "cell", cell.String(), "error", err)
		}
	})

	return nil
}
