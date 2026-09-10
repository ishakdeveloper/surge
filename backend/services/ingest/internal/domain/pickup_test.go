package domain_test

import (
	"testing"

	"github.com/ishakdeveloper/surge/services/ingest/internal/domain"
	"github.com/ishakdeveloper/surge/shared/geo"
	"github.com/ishakdeveloper/surge/shared/wire"
)

type pings struct {
	t     *testing.T
	index *domain.Index
	seq   uint64
}

func (p *pings) send(status wire.DriverStatus, at geo.Point, sentAtMs int64) domain.Observation {
	p.t.Helper()
	p.seq++
	observation, err := p.index.Observe(wire.DriverPing{
		DriverID: "drv-1", Epoch: 1, Seq: p.seq,
		Lat: at.Lat, Lng: at.Lng, Status: status, SentAtMs: sentAtMs,
	})
	if err != nil {
		p.t.Fatalf("observe: %v", err)
	}
	return observation
}

var (
	setOff  = geo.Point{Lat: 52.3700, Lng: 4.8900}
	halfway = geo.Point{Lat: 52.3730, Lng: 4.8950}
	kerb    = geo.Point{Lat: 52.3760, Lng: 4.9000}
)

// The first enroute_pickup ping starts the clock where the driver was, and the
// first on_trip ping stops it where they arrived: one pickup, timed by the
// driver's own clock, in the cell of the kerb.
func TestAPickupIsTimedFromSettingOffToArriving(t *testing.T) {
	p := &pings{t: t, index: domain.NewIndex()}
	p.send(wire.StatusIdle, setOff, 1_000)
	p.send(wire.StatusEnRoutePickup, setOff, 5_000)
	if observation := p.send(wire.StatusEnRoutePickup, halfway, 65_000); observation.Pickup != nil {
		t.Fatalf("a pickup completed while still en route: %+v", observation.Pickup)
	}
	arrived := p.send(wire.StatusOnTrip, kerb, 125_000)

	pickup := arrived.Pickup
	if pickup == nil {
		t.Fatal("arriving did not complete a pickup")
	}
	cell, _ := geo.ShardCell(kerb)
	if pickup.Seconds != 120 || pickup.Cell != cell.String() || pickup.DriverID != "drv-1" {
		t.Errorf("pickup = %+v, want 120s in the kerb's cell", pickup)
	}
	if want := geo.DistanceMeters(setOff, kerb); pickup.Meters < want-1 || pickup.Meters > want+1 {
		t.Errorf("meters = %.0f, want the %.0f from setting off to the kerb", pickup.Meters, want)
	}
	if next := p.send(wire.StatusOnTrip, kerb, 129_000); next.Pickup != nil {
		t.Errorf("the same pickup was reported twice: %+v", next.Pickup)
	}
}

// On trip without having been seen setting off — ingest restarted mid-pickup,
// or the trip was abandoned and resumed — is no pickup rather than a guess.
func TestNoPickupWithoutSeeingTheDriverSetOff(t *testing.T) {
	p := &pings{t: t, index: domain.NewIndex()}
	p.send(wire.StatusIdle, setOff, 1_000)
	if observation := p.send(wire.StatusOnTrip, kerb, 60_000); observation.Pickup != nil {
		t.Errorf("a pickup from idle straight to on_trip: %+v", observation.Pickup)
	}

	p.send(wire.StatusEnRoutePickup, setOff, 70_000)
	p.send(wire.StatusIdle, halfway, 80_000) // cancelled
	if observation := p.send(wire.StatusOnTrip, kerb, 200_000); observation.Pickup != nil {
		t.Errorf("a cancelled pickup completed later: %+v", observation.Pickup)
	}
}
