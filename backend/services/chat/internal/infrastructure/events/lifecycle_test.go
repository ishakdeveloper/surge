package events_test

import (
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/chat/internal/infrastructure/events"
	"github.com/ishakdeveloper/surge/shared/wire"
)

// Which trip facts are chat's business: an acceptance opens, an end with a
// driver closes, and nothing else has anybody to talk to.
func TestTripOf(t *testing.T) {
	fact := func(tag, driver string) wire.TripFact {
		return wire.TripFact{Tag: tag, TripID: "trip-1", RiderID: "rider-1", DriverID: driver, AtMs: 1757592000000}
	}
	ended := time.UnixMilli(1757592000000)

	cases := []struct {
		fact     wire.TripFact
		relevant bool
		ended    bool
	}{
		{fact(wire.FactTripAccepted, "drv-1"), true, false},
		{fact(wire.FactTripCompleted, "drv-1"), true, true},
		{fact(wire.FactTripCancelled, "drv-1"), true, true},
		{fact(wire.FactTripCancelled, ""), false, false},
		{fact(wire.FactTripRequested, ""), false, false},
		{fact(wire.FactTripUnmatched, ""), false, false},
	}
	for _, c := range cases {
		trip, relevant := events.TripOf(c.fact)
		if relevant != c.relevant {
			t.Errorf("%s (driver %q): relevant=%v", c.fact.Tag, c.fact.DriverID, relevant)
			continue
		}
		if !relevant {
			continue
		}
		if trip.DriverID != "drv-1" || trip.RiderID != "rider-1" {
			t.Errorf("%s: trip %+v", c.fact.Tag, trip)
		}
		if c.ended != !trip.EndedAt.IsZero() || (c.ended && !trip.EndedAt.Equal(ended)) {
			t.Errorf("%s: ended at %v", c.fact.Tag, trip.EndedAt)
		}
	}
}
