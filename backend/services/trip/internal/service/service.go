// Package service is the trip business logic.
//
// It depends on `domain` interfaces and nothing else — no gRPC, no Kafka, no
// Postgres. Dependency inversion is doing real work here rather than being
// ceremony: every test below runs against the in-memory repository and a fake
// router, so the logic that decides what a rider is charged and whether a
// booking is duplicated is verified without a container anywhere.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/ishakdeveloper/surge/services/trip/internal/domain"
	"github.com/ishakdeveloper/surge/shared/geo"
)

// Router is the routing engine, as this service needs it. Narrower than the
// Valhalla client: a route between two points, and nothing else.
type Router interface {
	Route(ctx context.Context, from, to geo.Point) (RouteResult, error)
}

type RouteResult struct {
	Polyline6 string
	Meters    float64
	Seconds   int64
}

// Surge supplies the demand multiplier for a pickup point. Phase 5 computes it
// per cell from real supply and demand; until then an implementation returning
// 1.0 is honest and the interface means the seam already exists.
type Surge interface {
	MultiplierAt(ctx context.Context, pickup geo.Point) float64
}

// Matcher is how a created trip reaches the matching engine.
type Matcher interface {
	RequestMatch(ctx context.Context, trip *domain.Trip) error
}

// Notifier tells the people on a trip that it changed.
//
// Called after a change is stored, never before, and it returns nothing: a push
// that fails must not undo a transition that has already been committed, so
// there is nothing a caller could do with an error from it.
type Notifier interface {
	TripChanged(ctx context.Context, trip *domain.Trip)
}

type silent struct{}

func (silent) TripChanged(context.Context, *domain.Trip) {}

// FareStore holds quotes between preview and booking.
//
// Separate from the trip repository because a fare is not a trip: most quotes
// are never accepted, they expire in minutes, and writing every pin-drag to
// Postgres would make browsing the map a write workload.
type FareStore interface {
	Put(ctx context.Context, fare domain.Fare) error
	Get(ctx context.Context, id string) (domain.Fare, error)
}

var (
	ErrFareExpired  = errors.New("service: fare has expired")
	ErrFareNotFound = errors.New("service: fare not found")
	// ErrNotAssigned is a driver acting on a trip that is not theirs. The
	// handler reports it as NotFound: telling someone a trip exists that they
	// may not touch is itself a disclosure.
	ErrNotAssigned = errors.New("service: trip is not assigned to this driver")
)

type Service struct {
	trips          domain.Repository
	fares          FareStore
	router         Router
	surge          Surge
	matcher        Matcher
	notifier       Notifier
	requirePayment bool
	now            func() time.Time
}

type Options struct {
	Trips   domain.Repository
	Fares   FareStore
	Router  Router
	Surge   Surge
	Matcher Matcher
	// Notifier is optional; without one, changes are simply not pushed.
	Notifier Notifier
	// RequirePayment makes a booking wait in payment_pending until payments
	// reports the fare held, and only then asks for a driver. Off, a booking
	// is dispatched at once — the flow from before payments existed, and the
	// one a machine without a payments service still needs.
	RequirePayment bool
	// Now is injectable so time-dependent behaviour — fare expiry above all —
	// is tested by moving a variable rather than by sleeping.
	Now func() time.Time
}

func New(options Options) *Service {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	notifier := options.Notifier
	if notifier == nil {
		notifier = silent{}
	}
	return &Service{
		trips: options.Trips, fares: options.Fares, router: options.Router,
		surge: options.Surge, matcher: options.Matcher, notifier: notifier,
		requirePayment: options.RequirePayment, now: now,
	}
}

// FareTTL is how long a quote stands.
//
// Long enough for a rider to choose a vehicle class, short enough that a fare
// quoted before a surge event cannot be redeemed after it.
const FareTTL = 3 * time.Minute

// Preview quotes every vehicle class from one route.
//
// One routing call, not three: the road is the same whichever car drives it,
// and asking Valhalla per class would triple the cost of a rider dragging a pin
// for no difference in the answer.
func (s *Service) Preview(ctx context.Context, riderID string, pickup, dropoff geo.Point) ([]domain.Fare, RouteResult, error) {
	route, err := s.router.Route(ctx, pickup, dropoff)
	if err != nil {
		return nil, RouteResult{}, fmt.Errorf("service: preview route: %w", err)
	}

	multiplier := 1.0
	if s.surge != nil {
		multiplier = s.surge.MultiplierAt(ctx, pickup)
	}

	now := s.now()
	fares := make([]domain.Fare, 0, len(domain.Catalogue))

	for _, class := range domain.Catalogue {
		fare := domain.Fare{
			ID:              uuid.NewString(),
			PackageSlug:     class.Slug,
			TotalCents:      class.Quote(route.Meters, route.Seconds, multiplier),
			SurgeMultiplier: multiplier,
			ExpiresAt:       now.Add(FareTTL),
			// The route travels with the quote so booking cannot re-route and
			// quietly produce a different price than the one displayed.
			Polyline6: route.Polyline6,
			Meters:    route.Meters,
			Seconds:   route.Seconds,
			Pickup:    domain.Coordinate{Lat: pickup.Lat, Lng: pickup.Lng},
			Dropoff:   domain.Coordinate{Lat: dropoff.Lat, Lng: dropoff.Lng},
		}

		if err := s.fares.Put(ctx, fare); err != nil {
			return nil, RouteResult{}, fmt.Errorf("service: store fare: %w", err)
		}
		fares = append(fares, fare)
	}

	return fares, route, nil
}

// Create turns an accepted quote into a trip and asks for a driver.
func (s *Service) Create(ctx context.Context, riderID, fareID, idempotencyKey string) (*domain.Trip, error) {
	// The retry path, checked first. A rider on a phone network who taps twice,
	// or whose request times out and is resent, must get the trip they already
	// have rather than a second one and a second driver.
	if idempotencyKey != "" {
		existing, err := s.trips.FindByIdempotencyKey(ctx, riderID, idempotencyKey)
		switch {
		case err == nil:
			return existing, nil
		case !errors.Is(err, domain.ErrNotFound):
			return nil, fmt.Errorf("service: idempotency lookup: %w", err)
		}
	}

	fare, err := s.fares.Get(ctx, fareID)
	if errors.Is(err, ErrFareNotFound) {
		return nil, ErrFareNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("service: read fare: %w", err)
	}

	now := s.now()
	if now.After(fare.ExpiresAt) {
		// Refused rather than silently re-quoted: a rider who saw €14 and is
		// charged €21 has been lied to, even if the second number is correct.
		return nil, ErrFareExpired
	}

	status := domain.StatusRequested
	if s.requirePayment {
		// Held before dispatched. The matcher is asked for a driver only when
		// payments reports the fare on the rider's card: see PaymentAuthorized.
		status = domain.StatusPaymentPending
	}

	trip := &domain.Trip{
		ID:              uuid.NewString(),
		RiderID:         riderID,
		Status:          status,
		Pickup:          geo.Point{Lat: fare.Pickup.Lat, Lng: fare.Pickup.Lng},
		Dropoff:         geo.Point{Lat: fare.Dropoff.Lat, Lng: fare.Dropoff.Lng},
		Polyline6:       fare.Polyline6,
		Meters:          fare.Meters,
		Seconds:         fare.Seconds,
		TotalCents:      fare.TotalCents,
		SurgeMultiplier: fare.SurgeMultiplier,
		PackageSlug:     fare.PackageSlug,
		Currency:        domain.MarketCurrency,
		IdempotencyKey:  idempotencyKey,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	// The trip and the fact that it was requested are stored together, so
	// every booking a rider is told exists is one payments hears about.
	if err := s.trips.Create(ctx, trip, domain.FactsOf(trip, "")...); err != nil {
		return nil, fmt.Errorf("service: create trip: %w", err)
	}

	// Two writers raced on the same key: the insert did nothing, and the trip
	// that exists is the other one. Reading it back is the correct answer to
	// both callers.
	if idempotencyKey != "" {
		stored, err := s.trips.FindByIdempotencyKey(ctx, riderID, idempotencyKey)
		if err == nil && stored.ID != trip.ID {
			return stored, nil
		}
	}

	// Asked for asynchronously: matching takes as long as a driver takes to
	// answer, and a rider must not hold an open RPC for eight seconds to find
	// out. The answer arrives as a trip event.
	if !s.requirePayment {
		if err := s.matcher.RequestMatch(ctx, trip); err != nil {
			return nil, fmt.Errorf("service: request match: %w", err)
		}
	}

	s.notifier.TripChanged(ctx, trip)
	return trip, nil
}

// CancelReasonPaymentFailed is why a trip whose fare could not be held ended.
// payments knows the specific reason; the rider reads it from there.
const CancelReasonPaymentFailed = "payment_failed"

// errMovedOn is a payment fact for a trip no longer waiting on it.
var errMovedOn = errors.New("service: trip is no longer waiting for payment")

// PaymentAuthorized dispatches a trip whose fare is now held.
//
// A trip already requested is asked for again rather than ignored. That is a
// redelivery finishing what a crash between storing the move and asking the
// matcher left undone; the matcher's idempotency window, keyed by trip id,
// makes the second request harmless. Anything past requested has a driver
// and is left alone.
func (s *Service) PaymentAuthorized(ctx context.Context, tripID string) error {
	trip, err := s.transitionIf(ctx, tripID, domain.StatusRequested, func(trip *domain.Trip) error {
		if trip.Status != domain.StatusPaymentPending && trip.Status != domain.StatusRequested {
			return errMovedOn
		}
		return nil
	}, nil)
	if errors.Is(err, errMovedOn) {
		return nil
	}
	if err != nil {
		return err
	}

	if err := s.matcher.RequestMatch(ctx, trip); err != nil {
		return fmt.Errorf("service: request match: %w", err)
	}
	return nil
}

// PaymentFailed cancels a trip whose fare could not be held.
//
// Only a trip still waiting on its payment. A failure cannot arrive after
// dispatch by design, and if one ever does, a driver already on their way is
// not a trip to pull out from under them.
func (s *Service) PaymentFailed(ctx context.Context, tripID string) error {
	_, err := s.transitionIf(ctx, tripID, domain.StatusCancelled, func(trip *domain.Trip) error {
		if trip.Status != domain.StatusPaymentPending && trip.Status != domain.StatusCancelled {
			return errMovedOn
		}
		return nil
	}, func(trip *domain.Trip) {
		if trip.Status != domain.StatusCancelled {
			trip.CancelReason = CancelReasonPaymentFailed
		}
	})
	if errors.Is(err, errMovedOn) {
		return nil
	}
	return err
}

func (s *Service) Get(ctx context.Context, id string) (*domain.Trip, error) {
	return s.trips.Get(ctx, id)
}

// DefaultPageSize and MaxPageSize bound a listing.
//
// A client asking for everything gets a page. "Return the whole table" is not a
// request a public API should honour, and the cap is the server's job because
// the client has no idea how much history a rider has.
const (
	DefaultPageSize = 20
	MaxPageSize     = 100
)

// List returns a rider's trips, newest first.
func (s *Service) List(ctx context.Context, filter domain.ListFilter) (domain.Page, error) {
	switch {
	case filter.Limit <= 0:
		filter.Limit = DefaultPageSize
	case filter.Limit > MaxPageSize:
		filter.Limit = MaxPageSize
	}

	return s.trips.List(ctx, filter)
}

// Cancel ends a trip early. The reason is kept: "payment_failed" is one the
// rider has to be shown, not guessed at.
func (s *Service) Cancel(ctx context.Context, id, reason string) (*domain.Trip, error) {
	return s.transition(ctx, id, domain.StatusCancelled, func(trip *domain.Trip) {
		// A repeated cancel keeps the first reason. The trip ended once.
		if trip.Status != domain.StatusCancelled {
			trip.CancelReason = reason
		}
	})
}

// Matched records that the matcher found a driver.
func (s *Service) Matched(ctx context.Context, tripID, driverID string) (*domain.Trip, error) {
	return s.transitionIf(ctx, tripID, domain.StatusAccepted, func(trip *domain.Trip) error {
		// A second match naming a different driver is refused rather than
		// applied. The first driver is already on their way, and money follows
		// the driver id: overwriting it would pay whoever was matched last.
		if trip.DriverID != "" && trip.DriverID != driverID {
			return fmt.Errorf("%w: already assigned to another driver", domain.ErrInvalidTransition)
		}
		return nil
	}, func(trip *domain.Trip) {
		trip.DriverID = driverID
	})
}

// Unmatched records that it did not.
func (s *Service) Unmatched(ctx context.Context, tripID string) (*domain.Trip, error) {
	return s.transition(ctx, tripID, domain.StatusUnmatched, nil)
}

// Arrive records that the driver is at the pickup.
func (s *Service) Arrive(ctx context.Context, tripID, driverID string) (*domain.Trip, error) {
	return s.driverTransition(ctx, tripID, driverID, domain.StatusArrived)
}

// Start records that the rider is in the car.
func (s *Service) Start(ctx context.Context, tripID, driverID string) (*domain.Trip, error) {
	return s.driverTransition(ctx, tripID, driverID, domain.StatusInProgress)
}

// Complete records that the rider has been dropped off.
func (s *Service) Complete(ctx context.Context, tripID, driverID string) (*domain.Trip, error) {
	return s.driverTransition(ctx, tripID, driverID, domain.StatusCompleted)
}

// driverTransition is the driver's half of the lifecycle, and the rule that
// only the assigned driver may move a trip forward.
//
// Here rather than in the handler because it is a business rule about trips,
// not a fact about who is calling: any transport that reached this service
// would need the same check, and putting it where the data is means none can
// skip it.
func (s *Service) driverTransition(ctx context.Context, id, driverID string, to domain.Status) (*domain.Trip, error) {
	return s.transitionIf(ctx, id, to, func(trip *domain.Trip) error {
		if trip.DriverID == "" || trip.DriverID != driverID {
			return ErrNotAssigned
		}
		return nil
	}, nil)
}

func (s *Service) transition(ctx context.Context, id string, to domain.Status, mutate func(*domain.Trip)) (*domain.Trip, error) {
	return s.transitionIf(ctx, id, to, nil, mutate)
}

// conflictAttempts bounds how often a transition that lost a race is re-run
// against the state the winner left. Two is enough for the race that actually
// happens — a rider and a driver acting at once — and a bound means a trip
// hammered by a bug fails loudly instead of spinning.
const conflictAttempts = 3

// transitionIf is every state change: read, check, move, store with its facts,
// tell people.
//
// One path, so there is no transition that forgets to notify — which is the bug
// a separate Cancel method with its own copy of these five steps had waiting —
// and none that forgets the fact payments charges on.
func (s *Service) transitionIf(
	ctx context.Context, id string, to domain.Status,
	guard func(*domain.Trip) error, mutate func(*domain.Trip),
) (*domain.Trip, error) {
	for attempt := 1; ; attempt++ {
		trip, err := s.trips.Get(ctx, id)
		if err != nil {
			return nil, err
		}

		if guard != nil {
			if err := guard(trip); err != nil {
				return nil, err
			}
		}
		if mutate != nil {
			mutate(trip)
		}

		from := trip.Status
		if err := trip.Transition(to, s.now()); err != nil {
			return nil, err
		}

		err = s.trips.Update(ctx, trip, from, domain.FactsOf(trip, from)...)
		if errors.Is(err, domain.ErrConflict) && attempt < conflictAttempts {
			// Somebody else moved the trip between the read and the write.
			// Decide again against what they left: usually this move is now
			// refused, which is the right outcome and not an error to hide.
			continue
		}
		if err != nil {
			return nil, err
		}

		s.notifier.TripChanged(ctx, trip)
		return trip, nil
	}
}
