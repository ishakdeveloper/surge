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

	fleetpb "github.com/ishakdeveloper/surge/shared/proto/fleet"
	paymentspb "github.com/ishakdeveloper/surge/shared/proto/payments"
	trippb "github.com/ishakdeveloper/surge/shared/proto/trip"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/stats/opentelemetry"
)

// Clients are the downstream services.
type Clients struct {
	Trip     trippb.TripServiceClient
	Payments paymentspb.PaymentsServiceClient
	Fleet    fleetpb.FleetServiceClient

	trip      *grpc.ClientConn
	simulator *grpc.ClientConn
	payments  *grpc.ClientConn
	chat      *grpc.ClientConn
	fleet     *grpc.ClientConn
}

// Dial connects to everything the gateway needs.
//
// Non-blocking: gRPC reconnects on its own, and a gateway that refuses to start
// because the trip service is briefly down is a gateway that turns one
// service's restart into a total outage. Requests made while it is down fail
// individually, which is the right blast radius.
func Dial(tripAddr, simulatorAddr, paymentsAddr, chatAddr, fleetAddr string) (*Clients, error) {
	trip, err := dial("trip service", tripAddr)
	if err != nil {
		return nil, err
	}
	simulator, err := dial("simulator", simulatorAddr)
	if err != nil {
		_ = trip.Close()
		return nil, err
	}
	payments, err := dial("payments service", paymentsAddr)
	if err != nil {
		_ = trip.Close()
		_ = simulator.Close()
		return nil, err
	}
	chat, err := dial("chat service", chatAddr)
	if err != nil {
		_ = trip.Close()
		_ = simulator.Close()
		_ = payments.Close()
		return nil, err
	}
	fleet, err := dial("fleet service", fleetAddr)
	if err != nil {
		_ = trip.Close()
		_ = simulator.Close()
		_ = payments.Close()
		_ = chat.Close()
		return nil, err
	}
	return &Clients{
		Trip:     trippb.NewTripServiceClient(trip),
		Payments: paymentspb.NewPaymentsServiceClient(payments),
		Fleet:    fleetpb.NewFleetServiceClient(fleet),
		trip:     trip, simulator: simulator, payments: payments, chat: chat, fleet: fleet,
	}, nil
}

// roundRobin spreads calls across every address the name resolves to.
//
// gRPC's default is pick_first: one HTTP/2 connection to whichever address
// answered first, held for the life of the process. Behind a ClusterIP Service
// that is one trip pod taking every call from this gateway while the others
// idle. The Services are headless, so the name resolves to every pod, and this
// balances across them.
const roundRobin = `{"loadBalancingConfig":[{"round_robin":{}}]}`

func dial(name, addr string) (*grpc.ClientConn, error) {
	conn, err := grpc.NewClient(addr,
		grpc.WithDefaultServiceConfig(roundRobin),
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
func (c *Clients) PaymentsConn() *grpc.ClientConn  { return c.payments }
func (c *Clients) ChatConn() *grpc.ClientConn      { return c.chat }
func (c *Clients) FleetConn() *grpc.ClientConn     { return c.fleet }

func (c *Clients) Close() {
	_ = c.trip.Close()
	_ = c.simulator.Close()
	_ = c.payments.Close()
	_ = c.chat.Close()
	_ = c.fleet.Close()
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
