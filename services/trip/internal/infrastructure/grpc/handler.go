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

	"github.com/ishakdeveloper/surge/shared/geo"
	commonpb "github.com/ishakdeveloper/surge/shared/proto/common"
	trippb "github.com/ishakdeveloper/surge/shared/proto/trip"
	"github.com/ishakdeveloper/surge/trip/internal/domain"
	"github.com/ishakdeveloper/surge/trip/internal/service"
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
	if request.GetPickup() == nil || request.GetDropoff() == nil {
		return nil, status.Error(codes.InvalidArgument, "pickup and dropoff are required")
	}

	fares, route, err := h.service.Preview(ctx, request.GetRiderId(),
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
	trip, err := h.service.Create(ctx, request.GetRiderId(), request.GetFareId(), request.GetIdempotencyKey())

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
	trip, err := h.service.Get(ctx, request.GetTripId())
	if errors.Is(err, domain.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "unknown trip")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not read trip: %v", err)
	}
	return &trippb.GetTripResponse{Trip: toProto(trip)}, nil
}

func (h *Handler) CancelTrip(ctx context.Context, request *trippb.CancelTripRequest) (*trippb.CancelTripResponse, error) {
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
