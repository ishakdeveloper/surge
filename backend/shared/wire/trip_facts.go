package wire

// Facts on `trip.lifecycle`: what happened to a trip, for the services that act
// on it without owning it.
//
// Separate from `trip.events`, which is the trip service's input from the
// matcher. Publishing these there would feed the trip service its own facts
// back, and make every consumer of either stream filter out the other's.
const (
	// FactTripRequested is a booking the rider has committed to. Payments
	// places a hold for TotalCents on it.
	FactTripRequested = "TripRequested"
	// FactTripAccepted is a driver assigned to the trip. Chat opens the
	// conversation between the rider and that driver on it.
	FactTripAccepted = "TripAccepted"
	// FactTripCompleted is a ride that happened. Payments captures on it.
	FactTripCompleted = "TripCompleted"
	// FactTripCancelled is a trip ended early, by the rider, by ops, or by a
	// payment that failed. Payments releases the hold.
	FactTripCancelled = "TripCancelled"
	// FactTripUnmatched is a trip nobody took. Payments releases the hold; the
	// rider's retry is a new TripRequested.
	FactTripUnmatched = "TripUnmatched"
)

// TripFact is the envelope on `trip.lifecycle`, keyed by trip id.
//
// Flat, and every fact carries the whole money-relevant snapshot rather than
// only what changed. A consumer that sees TripCompleted without having seen
// TripRequested — because it started after, or its group was reset — can still
// act on it without a lookup.
type TripFact struct {
	Tag      string `json:"_tag"`
	TripID   string `json:"tripId"`
	RiderID  string `json:"riderId"`
	DriverID string `json:"driverId"`

	TotalCents int64  `json:"totalCents"`
	Currency   string `json:"currency"`

	// Reason is why a trip was cancelled, and empty otherwise.
	Reason string `json:"reason"`

	AtMs int64 `json:"atMs"`
}

// Valid rejects a fact no consumer could act on.
func (f TripFact) Valid() bool {
	if f.TripID == "" || f.RiderID == "" {
		return false
	}
	switch f.Tag {
	case FactTripRequested, FactTripCancelled, FactTripUnmatched:
		return true
	case FactTripCompleted:
		// A completed trip had a driver, or there is nobody to pay.
		return f.DriverID != ""
	case FactTripAccepted:
		// An acceptance is the driver; without one there is nobody to talk to.
		return f.DriverID != ""
	default:
		return false
	}
}
