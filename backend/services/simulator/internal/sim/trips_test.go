package sim

import (
	"math/rand/v2"
	"testing"

	"github.com/ishakdeveloper/surge/shared/geo"
	"github.com/ishakdeveloper/surge/shared/routing"
	"github.com/ishakdeveloper/surge/shared/wire"
)

// An acceptance that sticks hands the trip to its driver exactly once; one
// that loses the race to another driver hands it to nobody.
func TestACommittedAcceptanceGoesToTheDriverOnce(t *testing.T) {
	policy := NewOfferPolicy(DefaultRiderConfig())
	var handed []wire.Offer
	policy.OnCommitted(func(offer wire.Offer) { handed = append(handed, offer) })

	first := wire.Offer{TripID: "t1", DriverID: "drv-000001"}
	if !policy.Commit(first) {
		t.Fatal("the first acceptance did not commit")
	}
	if policy.Commit(wire.Offer{TripID: "t1", DriverID: "drv-000002"}) {
		t.Fatal("a second driver took a trip already held")
	}
	if len(handed) != 1 || handed[0] != first {
		t.Errorf("handed %+v, want the first driver's offer once", handed)
	}
}

func lineThrough(t *testing.T, from, to geo.Point) *routing.Route {
	t.Helper()
	path, err := geo.NewPath([]geo.Point{from, to})
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	return &routing.Route{Path: path}
}

// A road passing about 150 m from Centraal: hotspot pickups land on it within
// the radius, and a hotspot no road passes near yields nothing rather than a
// point in the water.
func TestHotspotPickupsStayNearTheHotspot(t *testing.T) {
	pool := &RoutePool{routes: []*routing.Route{
		lineThrough(t, geo.Point{Lat: 52.37, Lng: 4.88}, geo.Point{Lat: 52.39, Lng: 4.92}),
	}}
	source := rand.New(rand.NewPCG(1, 2))
	centraal := Hotspots[0]

	for range 20 {
		point, ok := pool.PointNear(source, centraal, hotspotRadius)
		if !ok {
			t.Fatal("no pickup found on a road that passes the hotspot")
		}
		if d := geo.DistanceMeters(point, centraal); d > hotspotRadius {
			t.Fatalf("pickup %.0f m from the hotspot, radius %.0f", d, hotspotRadius)
		}
	}
	if _, ok := pool.PointNear(source, geo.Point{Lat: 52.30, Lng: 4.75}, hotspotRadius); ok {
		t.Error("found a pickup near a hotspot no road passes")
	}
}

// A trip's destination is about as far as asked: of a few points on a long
// road, the one nearest the target distance.
func TestTripsEndAboutTheRequestedDistanceAway(t *testing.T) {
	fleet := &Sim{pool: &RoutePool{routes: []*routing.Route{
		lineThrough(t, geo.Point{Lat: 52.33, Lng: 4.85}, geo.Point{Lat: 52.40, Lng: 4.95}),
	}}}
	source := rand.New(rand.NewPCG(3, 4))
	pickup := geo.Point{Lat: 52.365, Lng: 4.90}

	for range 10 {
		destination := fleet.destination(source, pickup, 1500)
		if d := geo.DistanceMeters(pickup, destination); d < 900 || d > 2100 {
			t.Errorf("trip of %.0f m, want about 1500", d)
		}
	}
}
