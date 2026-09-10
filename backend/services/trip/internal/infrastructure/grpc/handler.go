// Package grpc is the trip service's synchronous edge.
//
// A translation layer and nothing more: proto in, domain out, domain in, proto
// out. Keeping business logic out of it is what lets the service be exercised
// by tests that never open a socket, and would let a second transport be added
// without moving any of it.
package grpc

import (
	"context"
	"errors"

	"github.com/ishakdeveloper/surge/services/trip/internal/domain"
	"github.com/ishakdeveloper/surge/services/trip/internal/infrastructure/wirestatus"
	"github.com/ishakdeveloper/surge/services/trip/internal/service"
	"github.com/ishakdeveloper/surge/shared/authz"
	"github.com/ishakdeveloper/surge/shared/geo"
	commonpb "github.com/ishakdeveloper/surge/shared/proto/common"
	trippb "github.com/ishakdeveloper/surge/shared/proto/trip"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Handler struct {
	trippb.UnimplementedTripServiceServer
	service *service.Service
}

func NewHandler(trips *service.Service) *Handler { return &Handler{service: trips} }

func (h *Handler) PreviewTrip(ctx context.Context, request *trippb.PreviewTripRequest) (*trippb.PreviewTripResponse, error) {
	// The caller comes from metadata the gateway forwarded, never from the
	// request. With the REST body mapping straight onto this message, a
	// rider_id field would be client-supplied — and a client that can name the
	// rider can quote and book as somebody else.
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}

	if request.GetPickup() == nil || request.GetDropoff() == nil {
		return nil, status.Error(codes.InvalidArgument, "pickup and dropoff are required")
	}

	fares, route, err := h.service.Preview(ctx, caller.UserID,
		point(request.GetPickup()), point(request.GetDropoff()))
	if err != nil {
		// A pickup in the IJ is the caller's problem, not an outage. Mapping it
		// to Unavailable would make a client retry something that will never
		// succeed.
		return nil, status.Errorf(codes.InvalidArgument, "no route between those points: %v", err)
	}

	quotes := make([]*trippb.FareQuote, 0, len(fares))
	for _, fare := range fares {
		quotes = append(quotes, &trippb.FareQuote{
			FareId:          fare.ID,
			PackageSlug:     fare.PackageSlug,
			TotalCents:      fare.TotalCents,
			SurgeMultiplier: fare.SurgeMultiplier,
			ExpiresAt:       timestamppb.New(fare.ExpiresAt),
		})
	}

	return &trippb.PreviewTripResponse{
		Fares: quotes,
		Route: &commonpb.Route{
			Polyline6: route.Polyline6,
			Meters:    route.Meters,
			Seconds:   route.Seconds,
		},
	}, nil
}

func (h *Handler) CreateTrip(ctx context.Context, request *trippb.CreateTripRequest) (*trippb.CreateTripResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}

	// Over REST the key arrives as an Idempotency-Key header, which the gateway
	// forwards as metadata; a direct gRPC caller sets the field. Either is
	// fine, neither is optional.
	key := request.GetIdempotencyKey()
	if key == "" {
		key = authz.IdempotencyKeyFromMetadata(ctx)
	}
	if key == "" {
		return nil, status.Error(codes.InvalidArgument,
			"an Idempotency-Key is required so a retry cannot book twice")
	}

	trip, err := h.service.Create(ctx, caller.UserID, request.GetFareId(), key)

	switch {
	case errors.Is(err, service.ErrFareExpired):
		// FailedPrecondition, not InvalidArgument: the request was well formed
		// and would have worked a minute ago. The client should re-preview,
		// which is a different action from fixing its input.
		return nil, status.Error(codes.FailedPrecondition, "that fare has expired, please get a new quote")
	case errors.Is(err, service.ErrFareNotFound):
		return nil, status.Error(codes.NotFound, "unknown fare")
	case err != nil:
		return nil, status.Errorf(codes.Internal, "could not create trip: %v", err)
	}

	return &trippb.CreateTripResponse{Trip: toProto(trip)}, nil
}

func (h *Handler) GetTrip(ctx context.Context, request *trippb.GetTripRequest) (*trippb.GetTripResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}

	trip, err := h.service.Get(ctx, request.GetTripId())
	if errors.Is(err, domain.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "unknown trip")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not read trip: %v", err)
	}

	// Ownership is enforced here rather than at the gateway. The gateway knows
	// who is asking; only this service knows whose trip it is, and putting the
	// check where the data is means a second caller — the console, a mobile
	// app, a direct gRPC client — cannot skip it by not being the gateway.
	if !canSee(caller, trip) {
		// NotFound, not PermissionDenied: confirming that a trip exists to
		// someone who may not see it is itself a disclosure.
		return nil, status.Error(codes.NotFound, "unknown trip")
	}

	return &trippb.GetTripResponse{Trip: toProto(trip)}, nil
}

// canSee is the read rule: the rider who booked it, the driver assigned to it,
// or ops. The driver clause guards the empty id explicitly — an unassigned trip
// has DriverID "", and that must not match anybody.
func canSee(caller authz.Identity, trip *domain.Trip) bool {
	return trip.RiderID == caller.UserID ||
		(trip.DriverID != "" && trip.DriverID == caller.UserID) ||
		caller.Role == authz.RoleOps
}

func (h *Handler) ListTrips(ctx context.Context, request *trippb.ListTripsRequest) (*trippb.ListTripsResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}

	// The caller lists their own trips and nobody else's. There is no
	// parameter for whose history to read, which is the simplest way to ensure
	// there is no way to ask for someone else's — the token decides, and so
	// does which side of a trip the token belongs to.
	filter := domain.ListFilter{
		Status: domain.Status(request.GetStatus()),
		Limit:  int(request.GetPageSize()),
		Cursor: request.GetPageToken(),
	}
	if caller.Role == authz.RoleDriver {
		filter.DriverID = caller.UserID
	} else {
		filter.RiderID = caller.UserID
	}

	page, err := h.service.List(ctx, filter)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not list trips: %v", err)
	}

	trips := make([]*trippb.Trip, 0, len(page.Trips))
	for i := range page.Trips {
		trips = append(trips, toProto(&page.Trips[i]))
	}

	return &trippb.ListTripsResponse{Trips: trips, NextPageToken: page.NextCursor}, nil
}

func (h *Handler) CancelTrip(ctx context.Context, request *trippb.CancelTripRequest) (*trippb.CancelTripResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}

	// Read before cancelling, so a rider cannot cancel a stranger's ride by
	// guessing an id.
	existing, err := h.service.Get(ctx, request.GetTripId())
	if errors.Is(err, domain.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "unknown trip")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not read trip: %v", err)
	}
	if existing.RiderID != caller.UserID && caller.Role != authz.RoleOps {
		return nil, status.Error(codes.NotFound, "unknown trip")
	}

	trip, err := h.service.Cancel(ctx, request.GetTripId(), request.GetReason())

	switch {
	case errors.Is(err, domain.ErrNotFound):
		return nil, status.Error(codes.NotFound, "unknown trip")
	case errors.Is(err, domain.ErrInvalidTransition):
		return nil, status.Errorf(codes.FailedPrecondition, "this trip can no longer be cancelled: %v", err)
	case errors.Is(err, domain.ErrConflict):
		// Aborted is gRPC's "retry at a higher level": the trip kept changing
		// under this request, and asking again against fresh state is correct.
		return nil, status.Error(codes.Aborted, "this trip changed while cancelling it, please try again")
	case err != nil:
		return nil, status.Errorf(codes.Internal, "could not cancel trip: %v", err)
	}

	return &trippb.CancelTripResponse{Trip: toProto(trip)}, nil
}

func (h *Handler) ArriveTrip(ctx context.Context, request *trippb.ArriveTripRequest) (*trippb.ArriveTripResponse, error) {
	trip, err := h.driverAction(ctx, request.GetTripId(), h.service.Arrive)
	if err != nil {
		return nil, err
	}
	return &trippb.ArriveTripResponse{Trip: toProto(trip)}, nil
}

func (h *Handler) StartTrip(ctx context.Context, request *trippb.StartTripRequest) (*trippb.StartTripResponse, error) {
	trip, err := h.driverAction(ctx, request.GetTripId(), h.service.Start)
	if err != nil {
		return nil, err
	}
	return &trippb.StartTripResponse{Trip: toProto(trip)}, nil
}

func (h *Handler) CompleteTrip(ctx context.Context, request *trippb.CompleteTripRequest) (*trippb.CompleteTripResponse, error) {
	trip, err := h.driverAction(ctx, request.GetTripId(), h.service.Complete)
	if err != nil {
		return nil, err
	}
	return &trippb.CompleteTripResponse{Trip: toProto(trip)}, nil
}

// driverAction is the shared shape of the three driver transitions: a driver,
// a trip id, and the translation of what can go wrong into status codes.
func (h *Handler) driverAction(
	ctx context.Context, tripID string,
	action func(ctx context.Context, tripID, driverID string) (*domain.Trip, error),
) (*domain.Trip, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}
	if caller.Role != authz.RoleDriver {
		return nil, status.Error(codes.PermissionDenied, "only a driver can move a trip forward")
	}

	trip, err := action(ctx, tripID, caller.UserID)

	switch {
	case errors.Is(err, domain.ErrNotFound), errors.Is(err, service.ErrNotAssigned):
		return nil, status.Error(codes.NotFound, "unknown trip")
	case errors.Is(err, domain.ErrInvalidTransition):
		// FailedPrecondition: the request was well formed, the trip is simply
		// not in a state it can move from — "start" before "arrive", or twice
		// "complete" from a retry after the first one landed.
		return nil, status.Errorf(codes.FailedPrecondition, "this trip cannot do that now: %v", err)
	case errors.Is(err, domain.ErrConflict):
		return nil, status.Error(codes.Aborted, "this trip changed while updating it, please try again")
	case err != nil:
		return nil, status.Errorf(codes.Internal, "could not update trip: %v", err)
	}

	return trip, nil
}

func point(c *commonpb.Coordinate) geo.Point {
	return geo.Point{Lat: c.GetLat(), Lng: c.GetLng()}
}

func coordinate(p geo.Point) *commonpb.Coordinate {
	return &commonpb.Coordinate{Lat: p.Lat, Lng: p.Lng}
}

func toProto(trip *domain.Trip) *trippb.Trip {
	return &trippb.Trip{
		Id:         trip.ID,
		RiderId:    trip.RiderID,
		DriverId:   trip.DriverID,
		Status:     wirestatus.Of(trip.Status),
		Pickup:     coordinate(trip.Pickup),
		Dropoff:    coordinate(trip.Dropoff),
		Route:      &commonpb.Route{Polyline6: trip.Polyline6, Meters: trip.Meters, Seconds: trip.Seconds},
		TotalCents: trip.TotalCents,
		CreatedAt:  timestamppb.New(trip.CreatedAt),
		UpdatedAt:  timestamppb.New(trip.UpdatedAt),
	}
}
