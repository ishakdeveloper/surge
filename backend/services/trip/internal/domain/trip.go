// Package domain is the trip lifecycle, expressed without reference to how it
// is stored or transported.
//
// Nothing here imports gRPC, Postgres or Kafka. That is the whole point of the
// layering the reference uses: the state machine below is the part worth being
// sure about, and it can be tested by calling functions rather than by standing
// up a database.
package domain

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ishakdeveloper/surge/shared/geo"
)

// Status is where a trip is. The transitions between these are the only thing
// that may change a trip.
type Status string

const (
	// StatusPaymentPending is a booking waiting for its fare to be held on the
	// rider's card. Nobody is dispatched to a ride that cannot be paid for.
	StatusPaymentPending Status = "payment_pending"

	StatusRequested  Status = "requested"
	StatusOffered    Status = "offered"
	StatusAccepted   Status = "accepted"
	StatusArrived    Status = "arrived"
	StatusInProgress Status = "in_progress"
	StatusCompleted  Status = "completed"
	StatusCancelled  Status = "cancelled"
	// StatusUnmatched means nobody was found. Distinct from cancelled because
	// nobody chose it — the rider should be offered a retry, not an apology.
	StatusUnmatched Status = "unmatched"
)

// transitions is the state machine, written down once.
//
// A map rather than a switch because it is data: it can be printed, tested
// exhaustively, and rendered as the diagram in docs/architecture without
// anybody transcribing it.
var transitions = map[Status][]Status{
	// Only forward to dispatch once the fare is held, or out. There is no path
	// from here to accepted: a driver cannot be assigned to a ride nobody has
	// shown they can pay for.
	StatusPaymentPending: {StatusRequested, StatusCancelled},

	// Requested goes straight to accepted as well as through offered, because
	// the trip service does not observe every offer. `offered` is the matcher's
	// internal state — a trip may be offered to four drivers in turn — and what
	// reaches here is the outcome. Only when the gateway starts pushing "finding
	// you a driver" does the intermediate state become something this service
	// needs to hold.
	StatusRequested:  {StatusOffered, StatusAccepted, StatusCancelled, StatusUnmatched},
	StatusOffered:    {StatusAccepted, StatusRequested, StatusCancelled, StatusUnmatched},
	StatusAccepted:   {StatusArrived, StatusCancelled},
	StatusArrived:    {StatusInProgress, StatusCancelled},
	StatusInProgress: {StatusCompleted},
	StatusCompleted:  {},
	StatusCancelled:  {},
	StatusUnmatched:  {StatusRequested},
}

// ErrInvalidTransition is returned rather than panicking, because the caller is
// usually a redelivered message rather than a bug: at-least-once delivery means
// "accepted" can arrive twice, and the second one is not a crash.
var ErrInvalidTransition = errors.New("trip: invalid transition")

// ErrNotFound is a trip that does not exist.
var ErrNotFound = errors.New("trip: not found")

// ErrConflict is a write that lost a race: the trip changed between being read
// and being stored. The caller re-reads and decides again, which usually means
// the move it wanted is now refused — a rider's cancel that loses to a driver's
// accept is a cancel of an accepted trip, not an accept that silently vanished.
var ErrConflict = errors.New("trip: changed concurrently")

// MarketCurrency is what Amsterdam prices are in. One market, one currency, but
// a price without a currency is a number rather than an amount, so every trip
// carries it.
const MarketCurrency = "eur"

// CanTransition reports whether a move is legal.
func CanTransition(from, to Status) bool {
	for _, allowed := range transitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// Trip is the aggregate.
type Trip struct {
	ID       string
	RiderID  string
	DriverID string
	Status   Status

	Pickup  geo.Point
	Dropoff geo.Point

	Polyline6 string
	Meters    float64
	Seconds   int64

	TotalCents      int64
	Currency        string
	SurgeMultiplier float64
	PackageSlug     string

	// IdempotencyKey is what makes a retried booking return the original trip
	// rather than creating a second one.
	IdempotencyKey string

	// CancelReason is why a cancelled trip was cancelled, and empty otherwise.
	CancelReason string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Transition moves the trip, or explains why it cannot.
func (t *Trip) Transition(to Status, now time.Time) error {
	if t.Status == to {
		// Idempotent by design. A redelivered event that asks for the state the
		// trip is already in has nothing to do, and treating that as an error
		// would make at-least-once delivery noisy for no benefit.
		return nil
	}
	if !CanTransition(t.Status, to) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, t.Status, to)
	}

	t.Status = to
	t.UpdatedAt = now
	return nil
}

// Terminal reports whether the trip can still change.
func (t *Trip) Terminal() bool { return len(transitions[t.Status]) == 0 }

// Repository is persistence, as the service sees it.
//
// An interface in the domain, implemented in infrastructure: the service
// depends on this, and Postgres depends on the service's needs rather than the
// other way round. It is also what lets the whole service be tested against an
// in-memory implementation with no container.
// Page is a cursor-paginated slice of trips.
//
// A cursor rather than an offset. Offsets shift under you: a trip completed
// between page one and page two moves everything down, and the rider sees a
// duplicate or misses one entirely. A cursor over (created_at, id) is stable
// whatever happens in between.
type Page struct {
	Trips []Trip
	// NextCursor is empty when there are no more.
	NextCursor string
}

// ListFilter narrows a caller's history.
//
// Exactly one of RiderID and DriverID is set, by whoever built the filter from a
// verified caller. Both repositories match on whichever is set and on nothing
// when neither is — so a filter built wrongly lists no trips rather than every
// trip, which is the only acceptable way for that bug to fail.
type ListFilter struct {
	RiderID  string
	DriverID string
	// Status is optional; empty means every status.
	Status Status
	Limit  int
	Cursor string
}

type Repository interface {
	// Create stores a new trip and the facts it makes, together. A booking
	// whose idempotency key already exists stores neither: its facts were
	// stored when it was.
	Create(ctx context.Context, trip *Trip, facts ...Fact) error
	Get(ctx context.Context, id string) (*Trip, error)
	List(ctx context.Context, filter ListFilter) (Page, error)
	// FindByIdempotencyKey returns the trip a key already created, or
	// ErrNotFound. This is what makes CreateTrip safe to retry.
	FindByIdempotencyKey(ctx context.Context, riderID, key string) (*Trip, error)
	// Update stores a move out of `from`, and its facts, together — or returns
	// ErrConflict if the trip is no longer in `from` when the write lands.
	Update(ctx context.Context, trip *Trip, from Status, facts ...Fact) error
}

// FactKind is a change other services act on.
type FactKind string

const (
	FactRequested FactKind = "requested"
	FactCompleted FactKind = "completed"
	FactCancelled FactKind = "cancelled"
	FactUnmatched FactKind = "unmatched"
)

// Fact is a change worth telling the rest of the system about, with the trip
// as it stood once the change was made.
//
// Most transitions are not facts. Accepted, arrived and in-progress matter to
// the people on the trip, who hear about them as pushes; a fact is the handful
// of moves another service does something about — payments places a hold on a
// request, captures on completion, and lets go on a cancel.
type Fact struct {
	Kind FactKind
	Trip Trip
}

// FactsOf returns the facts made by moving a trip from `from` to the status it
// is in now. An empty `from` means the trip was just created.
//
// Decided here rather than by whoever stores the trip, because which moves are
// news is a rule about trips, and the Postgres and in-memory repositories must
// not be able to disagree about it.
func FactsOf(trip *Trip, from Status) []Fact {
	if from == trip.Status {
		// A redelivered event asking for the state the trip is already in
		// changes nothing, and a second "completed" would be a second capture
		// request for one ride.
		return nil
	}

	var kind FactKind
	switch {
	case from == "", from == StatusUnmatched && trip.Status == StatusRequested:
		kind = FactRequested
	case trip.Status == StatusCompleted:
		kind = FactCompleted
	case trip.Status == StatusCancelled:
		kind = FactCancelled
	case trip.Status == StatusUnmatched:
		kind = FactUnmatched
	default:
		return nil
	}
	return []Fact{{Kind: kind, Trip: *trip}}
}
