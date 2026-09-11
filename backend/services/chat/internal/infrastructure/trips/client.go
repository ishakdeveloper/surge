// Package trips is chat's client for the trip service: whose trip it is, and
// whether it has a driver yet.
package trips

import (
	"context"
	"fmt"
	"time"

	"github.com/ishakdeveloper/surge/services/chat/internal/service"
	"github.com/ishakdeveloper/surge/shared/authz"
	trippb "github.com/ishakdeveloper/surge/shared/proto/trip"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/stats/opentelemetry"
	"google.golang.org/grpc/status"
)

// timeout bounds a lookup. It sits in front of a person opening a chat, and a
// wedged trip service should cost them seconds, not the request's lifetime.
const timeout = 3 * time.Second

type Client struct {
	conn  *grpc.ClientConn
	trips trippb.TripServiceClient
}

// Dial connects without blocking, as the gateway does: gRPC reconnects on its
// own, and chat refusing to start while trip restarts would turn one restart
// into two outages.
func Dial(addr string) (*Client, error) {
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		opentelemetry.DialOption(opentelemetry.Options{}),
	)
	if err != nil {
		return nil, fmt.Errorf("trips: dial %s: %w", addr, err)
	}
	return &Client{conn: conn, trips: trippb.NewTripServiceClient(conn)}, nil
}

func (c *Client) Close() { _ = c.conn.Close() }

// Trip asks as the caller rather than as chat, so the trip service's own rule
// — the rider, the assigned driver, or ops — decides, and chat keeps no copy
// of it to drift.
func (c *Client) Trip(ctx context.Context, caller authz.Identity, tripID string) (service.Trip, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	response, err := c.trips.GetTrip(authz.Outgoing(ctx, caller), &trippb.GetTripRequest{TripId: tripID})
	if status.Code(err) == codes.NotFound {
		return service.Trip{}, service.ErrTripNotFound
	}
	if err != nil {
		return service.Trip{}, fmt.Errorf("trips: get %s: %w", tripID, err)
	}

	trip := response.GetTrip()
	out := service.Trip{ID: trip.GetId(), RiderID: trip.GetRiderId(), DriverID: trip.GetDriverId()}
	switch trip.GetStatus() {
	case trippb.TripStatus_TRIP_STATUS_COMPLETED, trippb.TripStatus_TRIP_STATUS_CANCELLED:
		out.EndedAt = trip.GetUpdatedAt().AsTime()
	}
	return out, nil
}
