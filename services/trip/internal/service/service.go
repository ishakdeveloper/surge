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
	"github.com/ishakdeveloper/surge/shared/geo"
	"github.com/ishakdeveloper/surge/trip/internal/domain"
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
)

type Service struct {
	trips   domain.Repository
	fares   FareStore
	router  Router
	surge   Surge
	matcher Matcher
	now     func() time.Time
}

type Options struct {
	Trips   domain.Repository
	Fares   FareStore
	Router  Router
	Surge   Surge
	Matcher Matcher
	// Now is injectable so time-dependent behaviour — fare expiry above all —
	// is tested by moving a variable rather than by sleeping.
	Now func() time.Time
}

func New(options Options) *Service {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		trips: options.Trips, fares: options.Fares, router: options.Router,
		surge: options.Surge, matcher: options.Matcher, now: now,
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

	trip := &domain.Trip{
		ID:              uuid.NewString(),
		RiderID:         riderID,
		Status:          domain.StatusRequested,
		Pickup:          geo.Point{Lat: fare.Pickup.Lat, Lng: fare.Pickup.Lng},
		Dropoff:         geo.Point{Lat: fare.Dropoff.Lat, Lng: fare.Dropoff.Lng},
		Polyline6:       fare.Polyline6,
		Meters:          fare.Meters,
		Seconds:         fare.Seconds,
		TotalCents:      fare.TotalCents,
		SurgeMultiplier: fare.SurgeMultiplier,
		PackageSlug:     fare.PackageSlug,
		IdempotencyKey:  idempotencyKey,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if err := s.trips.Create(ctx, trip); err != nil {
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
	if err := s.matcher.RequestMatch(ctx, trip); err != nil {
		return nil, fmt.Errorf("service: request match: %w", err)
	}

	return trip, nil
}

func (s *Service) Get(ctx context.Context, id string) (*domain.Trip, error) {
	return s.trips.Get(ctx, id)
}

// Cancel ends a trip early.
func (s *Service) Cancel(ctx context.Context, id, reason string) (*domain.Trip, error) {
	trip, err := s.trips.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := trip.Transition(domain.StatusCancelled, s.now()); err != nil {
		return nil, err
	}
	if err := s.trips.Update(ctx, trip); err != nil {
		return nil, err
	}
	return trip, nil
}

// Matched records that the matcher found a driver.
func (s *Service) Matched(ctx context.Context, tripID, driverID string) (*domain.Trip, error) {
	return s.transition(ctx, tripID, domain.StatusAccepted, func(trip *domain.Trip) {
		trip.DriverID = driverID
	})
}

// Unmatched records that it did not.
func (s *Service) Unmatched(ctx context.Context, tripID string) (*domain.Trip, error) {
	return s.transition(ctx, tripID, domain.StatusUnmatched, nil)
}

func (s *Service) transition(ctx context.Context, id string, to domain.Status, mutate func(*domain.Trip)) (*domain.Trip, error) {
	trip, err := s.trips.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	if mutate != nil {
		mutate(trip)
	}
	if err := trip.Transition(to, s.now()); err != nil {
		return nil, err
	}
	if err := s.trips.Update(ctx, trip); err != nil {
		return nil, err
	}
	return trip, nil
}
