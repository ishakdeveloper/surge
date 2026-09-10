// Package grpc holds the gateway's clients for the services behind it.
//
// The gateway is the only process that speaks to browsers, and the only one
// that fans a browser request out to internal services. Everything below is a
// client, never a server.
package grpc

import (
	"context"
	"fmt"
	"time"

	trippb "github.com/ishakdeveloper/surge/shared/proto/trip"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/stats/opentelemetry"
)

// Clients are the downstream services.
type Clients struct {
	Trip trippb.TripServiceClient

	trip      *grpc.ClientConn
	simulator *grpc.ClientConn
}

// Dial connects to everything the gateway needs.
//
// Non-blocking: gRPC reconnects on its own, and a gateway that refuses to start
// because the trip service is briefly down is a gateway that turns one
// service's restart into a total outage. Requests made while it is down fail
// individually, which is the right blast radius.
func Dial(tripAddr, simulatorAddr string) (*Clients, error) {
	trip, err := dial("trip service", tripAddr)
	if err != nil {
		return nil, err
	}
	simulator, err := dial("simulator", simulatorAddr)
	if err != nil {
		_ = trip.Close()
		return nil, err
	}
	return &Clients{Trip: trippb.NewTripServiceClient(trip), trip: trip, simulator: simulator}, nil
}

func dial(name, addr string) (*grpc.ClientConn, error) {
	conn, err := grpc.NewClient(addr,
		// Plaintext inside the cluster. TLS belongs at the mesh or ingress; two
		// layers of certificate management for a hop that never leaves the
		// network is cost without benefit.
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff: backoff.Config{
				BaseDelay:  100 * time.Millisecond,
				Multiplier: 1.6,
				Jitter:     0.2,
				MaxDelay:   5 * time.Second,
			},
			MinConnectTimeout: 2 * time.Second,
		}),
		// Client-side tracing, so a browser request and the gRPC hop it causes
		// appear in one trace alongside the Kafka records that follow.
		opentelemetry.DialOption(opentelemetry.Options{}),
	)
	if err != nil {
		return nil, fmt.Errorf("gateway: dial %s at %s: %w", name, addr, err)
	}
	return conn, nil
}

// TripConn and SimulatorConn are the connections the generated REST gateway
// proxies onto.
//
// grpc-gateway registers against a ClientConn rather than a typed client,
// because it dispatches by method name from the annotations rather than by
// calling Go methods.
func (c *Clients) TripConn() *grpc.ClientConn      { return c.trip }
func (c *Clients) SimulatorConn() *grpc.ClientConn { return c.simulator }

func (c *Clients) Close() {
	_ = c.trip.Close()
	_ = c.simulator.Close()
}

// Timeout bounds a downstream call.
//
// Every gateway call gets one. A browser request that hangs because an internal
// service is wedged consumes a connection slot at the edge, and enough of them
// is how one slow service takes the gateway with it.
const Timeout = 8 * time.Second

func WithTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, Timeout)
}
