// Package control is the simulator's knob, served over gRPC so the gateway can
// put it behind the same REST surface, token and generated client as every
// other API.
package control

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/ishakdeveloper/surge/services/simulator/internal/sim"
	"github.com/ishakdeveloper/surge/shared/authz"
	simpb "github.com/ishakdeveloper/surge/shared/proto/sim"
)

const (
	// MaxDrivers is where the console's slider stops. Past it this laptop runs
	// out of file descriptors before the system under test runs out of
	// anything interesting.
	MaxDrivers = 50_000
	// MinPingInterval keeps a slip of the slider from asking ten thousand
	// drivers to report forty times a second.
	MinPingInterval = 250 * time.Millisecond
	MaxPingInterval = time.Minute
)

// Fleet is what the server drives: the running simulator.
type Fleet interface {
	Config() sim.Config
	Running() int
	Pings() uint64
	Scale(ctx context.Context, config sim.Config) error
}

type Server struct {
	simpb.UnimplementedSimulatorServiceServer

	// daemon is the simulator's own lifetime. A rescale starts drivers, and
	// starting them under the request's context would stop every one of them
	// the moment the response was written.
	daemon context.Context
	fleet  Fleet
}

func NewServer(daemon context.Context, fleet Fleet) *Server {
	return &Server{daemon: daemon, fleet: fleet}
}

func (s *Server) GetSimulator(ctx context.Context, _ *simpb.GetSimulatorRequest) (*simpb.GetSimulatorResponse, error) {
	if err := requireOps(ctx); err != nil {
		return nil, err
	}
	return &simpb.GetSimulatorResponse{Simulator: s.snapshot()}, nil
}

func (s *Server) ConfigureSimulator(ctx context.Context, request *simpb.ConfigureSimulatorRequest) (*simpb.ConfigureSimulatorResponse, error) {
	if err := requireOps(ctx); err != nil {
		return nil, err
	}

	interval := time.Duration(request.GetPingIntervalMs()) * time.Millisecond
	switch {
	case request.GetDrivers() < 0 || request.GetDrivers() > MaxDrivers:
		return nil, status.Errorf(codes.InvalidArgument, "drivers must be between 0 and %d", MaxDrivers)
	case interval < MinPingInterval || interval > MaxPingInterval:
		return nil, status.Errorf(codes.InvalidArgument, "ping interval must be between %s and %s", MinPingInterval, MaxPingInterval)
	}

	settings := s.fleet.Config()
	settings.Drivers = int(request.GetDrivers())
	settings.PingInterval = interval
	if err := s.fleet.Scale(s.daemon, settings); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "rescale: %v", err)
	}
	return &simpb.ConfigureSimulatorResponse{Simulator: s.snapshot()}, nil
}

func (s *Server) snapshot() *simpb.Simulator {
	settings := s.fleet.Config()
	rate := 0.0
	if settings.PingInterval > 0 {
		rate = float64(settings.Drivers) / settings.PingInterval.Seconds()
	}
	return &simpb.Simulator{
		Drivers:              int32(settings.Drivers),
		Running:              int32(s.fleet.Running()),
		PingsTotal:           int64(s.fleet.Pings()),
		PingIntervalMs:       int32(settings.PingInterval.Milliseconds()),
		TargetPingsPerSecond: rate,
	}
}

// requireOps is the whole permission model for the simulator: turning the
// fleet from 500 drivers to 50,000 is an operation on shared infrastructure.
func requireOps(ctx context.Context) error {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return err
	}
	if caller.Role != authz.RoleOps {
		return status.Error(codes.PermissionDenied, "the simulator is for ops")
	}
	return nil
}
