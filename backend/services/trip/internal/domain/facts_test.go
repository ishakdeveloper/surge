package domain_test

import (
	"testing"

	"github.com/ishakdeveloper/surge/services/trip/internal/domain"
)

// Which moves are news, written down as a table so a change to it is a
// visible diff rather than a quiet edit to a switch.
func TestFactsOf(t *testing.T) {
	cases := []struct {
		from, to domain.Status
		want     domain.FactKind // empty: no fact
	}{
		{"", domain.StatusRequested, domain.FactRequested},
		{domain.StatusRequested, domain.StatusAccepted, ""},
		{domain.StatusOffered, domain.StatusAccepted, ""},
		{domain.StatusAccepted, domain.StatusArrived, ""},
		{domain.StatusArrived, domain.StatusInProgress, ""},
		{domain.StatusInProgress, domain.StatusCompleted, domain.FactCompleted},
		{domain.StatusRequested, domain.StatusCancelled, domain.FactCancelled},
		{domain.StatusArrived, domain.StatusCancelled, domain.FactCancelled},
		{domain.StatusRequested, domain.StatusUnmatched, domain.FactUnmatched},
		{domain.StatusUnmatched, domain.StatusRequested, domain.FactRequested},
		// A redelivery is not news: a second completion would be a second
		// capture request for one ride.
		{domain.StatusCompleted, domain.StatusCompleted, ""},
		{domain.StatusAccepted, domain.StatusAccepted, ""},
	}

	for _, c := range cases {
		trip := &domain.Trip{ID: "trip-1", Status: c.to}
		facts := domain.FactsOf(trip, c.from)

		switch {
		case c.want == "" && len(facts) != 0:
			t.Errorf("%q -> %s: want no fact, got %v", c.from, c.to, facts[0].Kind)
		case c.want != "" && (len(facts) != 1 || facts[0].Kind != c.want):
			t.Errorf("%q -> %s: want %s, got %v", c.from, c.to, c.want, facts)
		case c.want != "" && facts[0].Trip.ID != trip.ID:
			t.Errorf("%q -> %s: the fact does not carry the trip", c.from, c.to)
		}
	}
}
