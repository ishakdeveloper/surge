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
	if trip.RiderID != caller.UserID && caller.Role != authz.RoleOps {
		// NotFound, not PermissionDenied: confirming that a trip exists to
		// someone who may not see it is itself a disclosure.
		return nil, status.Error(codes.NotFound, "unknown trip")
	}

	return &trippb.GetTripResponse{Trip: toProto(trip)}, nil
}

func (h *Handler) ListTrips(ctx context.Context, request *trippb.ListTripsRequest) (*trippb.ListTripsResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}

	page, err := h.service.List(ctx, domain.ListFilter{
		// A rider lists their own trips and nobody else's. There is no
		// parameter for whose history to read, which is the simplest way to
		// ensure there is no way to ask for someone else's.
		RiderID: caller.UserID,
		Status:  domain.Status(request.GetStatus()),
		Limit:   int(request.GetPageSize()),
		Cursor:  request.GetPageToken(),
	})
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
	case err != nil:
		return nil, status.Errorf(codes.Internal, "could not cancel trip: %v", err)
	}

	return &trippb.CancelTripResponse{Trip: toProto(trip)}, nil
}

func point(c *commonpb.Coordinate) geo.Point {
	return geo.Point{Lat: c.GetLat(), Lng: c.GetLng()}
}

func coordinate(p geo.Point) *commonpb.Coordinate {
	return &commonpb.Coordinate{Lat: p.Lat, Lng: p.Lng}
}

// statuses maps the domain's state names to the wire enum.
//
// An explicit table rather than a string cast, so renaming a domain constant is
// a compile error here instead of a silently UNSPECIFIED status on the wire.
var statuses = map[domain.Status]trippb.TripStatus{
	domain.StatusRequested:  trippb.TripStatus_TRIP_STATUS_REQUESTED,
	domain.StatusOffered:    trippb.TripStatus_TRIP_STATUS_OFFERED,
	domain.StatusAccepted:   trippb.TripStatus_TRIP_STATUS_ACCEPTED,
	domain.StatusArrived:    trippb.TripStatus_TRIP_STATUS_ARRIVED,
	domain.StatusInProgress: trippb.TripStatus_TRIP_STATUS_IN_PROGRESS,
	domain.StatusCompleted:  trippb.TripStatus_TRIP_STATUS_COMPLETED,
	domain.StatusCancelled:  trippb.TripStatus_TRIP_STATUS_CANCELLED,
	domain.StatusUnmatched:  trippb.TripStatus_TRIP_STATUS_UNMATCHED,
}

func toProto(trip *domain.Trip) *trippb.Trip {
	return &trippb.Trip{
		Id:         trip.ID,
		RiderId:    trip.RiderID,
		DriverId:   trip.DriverID,
		Status:     statuses[trip.Status],
		Pickup:     coordinate(trip.Pickup),
		Dropoff:    coordinate(trip.Dropoff),
		Route:      &commonpb.Route{Polyline6: trip.Polyline6, Meters: trip.Meters, Seconds: trip.Seconds},
		TotalCents: trip.TotalCents,
		CreatedAt:  timestamppb.New(trip.CreatedAt),
		UpdatedAt:  timestamppb.New(trip.UpdatedAt),
	}
}
