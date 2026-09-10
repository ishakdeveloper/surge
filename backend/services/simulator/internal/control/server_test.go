package control_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/ishakdeveloper/surge/services/simulator/internal/control"
	"github.com/ishakdeveloper/surge/services/simulator/internal/sim"
	"github.com/ishakdeveloper/surge/shared/authz"
	simpb "github.com/ishakdeveloper/surge/shared/proto/sim"
)

type fakeFleet struct {
	config sim.Config
	scaled []sim.Config
}

func (f *fakeFleet) Config() sim.Config { return f.config }
func (f *fakeFleet) Running() int       { return f.config.Drivers }
func (f *fakeFleet) Pings() uint64      { return 12345 }
func (f *fakeFleet) Scale(_ context.Context, config sim.Config) error {
	f.scaled = append(f.scaled, config)
	f.config = config
	return nil
}

func as(role authz.Role) context.Context {
	return authz.WithIdentity(context.Background(), authz.Identity{UserID: "u-1", Role: role})
}

func code(err error) codes.Code { return status.Code(err) }

// Only ops turn the knob. A rider is refused, and a caller with no identity is
// told to authenticate rather than that they are not allowed.
func TestOnlyOpsMayTouchTheSimulator(t *testing.T) {
	server := control.NewServer(context.Background(), &fakeFleet{config: sim.Config{Drivers: 10, PingInterval: time.Second}})

	if _, err := server.GetSimulator(context.Background(), &simpb.GetSimulatorRequest{}); code(err) != codes.Unauthenticated {
		t.Errorf("anonymous: got %v, want Unauthenticated", err)
	}
	if _, err := server.GetSimulator(as(authz.RoleRider), &simpb.GetSimulatorRequest{}); code(err) != codes.PermissionDenied {
		t.Errorf("rider: got %v, want PermissionDenied", err)
	}
	if _, err := server.ConfigureSimulator(as(authz.RoleDriver), &simpb.ConfigureSimulatorRequest{Drivers: 1, PingIntervalMs: 1000}); code(err) != codes.PermissionDenied {
		t.Errorf("driver rescaling: got %v, want PermissionDenied", err)
	}
}

// A rescale keeps the settings it was not asked about — the seed and speeds
// the daemon started with — and reports the rate it implies.
func TestConfigureRescalesAndKeepsTheRest(t *testing.T) {
	fleet := &fakeFleet{config: sim.Config{Drivers: 10, PingInterval: 4 * time.Second, Seed: 7, SpeedKmhMax: 50}}
	server := control.NewServer(context.Background(), fleet)

	response, err := server.ConfigureSimulator(as(authz.RoleOps), &simpb.ConfigureSimulatorRequest{Drivers: 2000, PingIntervalMs: 2000})
	if err != nil {
		t.Fatalf("configure: %v", err)
	}
	if len(fleet.scaled) != 1 || fleet.scaled[0].Seed != 7 || fleet.scaled[0].SpeedKmhMax != 50 {
		t.Errorf("rescale lost the other settings: %+v", fleet.scaled)
	}
	got := response.GetSimulator()
	if got.GetDrivers() != 2000 || got.GetPingIntervalMs() != 2000 || got.GetTargetPingsPerSecond() != 1000 {
		t.Errorf("simulator = %+v", got)
	}
}

// Out-of-range requests are refused before anything is scaled.
func TestConfigureRejectsOutOfRange(t *testing.T) {
	fleet := &fakeFleet{config: sim.Config{Drivers: 10, PingInterval: time.Second}}
	server := control.NewServer(context.Background(), fleet)

	for _, request := range []*simpb.ConfigureSimulatorRequest{
		{Drivers: control.MaxDrivers + 1, PingIntervalMs: 1000},
		{Drivers: -1, PingIntervalMs: 1000},
		{Drivers: 100, PingIntervalMs: 10},
		{Drivers: 100, PingIntervalMs: 0},
	} {
		if _, err := server.ConfigureSimulator(as(authz.RoleOps), request); code(err) != codes.InvalidArgument {
			t.Errorf("%+v: got %v, want InvalidArgument", request, err)
		}
	}
	if len(fleet.scaled) != 0 {
		t.Errorf("an invalid request rescaled the fleet: %+v", fleet.scaled)
	}
}
