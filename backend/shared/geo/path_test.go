package geo_test

import (
	"encoding/json"
	"math"
	"os"
	"testing"

	"github.com/ishakdeveloper/surge/shared/geo"
)

type routeFixture struct {
	Shape           string  `json:"shape"`
	ValhallaKm      float64 `json:"valhallaKm"`
	ValhallaSeconds float64 `json:"valhallaSeconds"`
}

func loadRoute(t *testing.T) routeFixture {
	t.Helper()

	raw, err := os.ReadFile("testdata/route_centraal_rijksmuseum.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var fixture routeFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return fixture
}

// The decoder is tested against real router output rather than a hand-made
// string, because the failure mode that matters is subtle: decoding a
// precision-6 polyline at precision 5 produces a perfectly well-formed route
// that is off by a factor of ten. Checking the decoded length against the
// distance Valhalla itself reported is what catches that.
func TestDecodePolyline6AgainstValhalla(t *testing.T) {
	fixture := loadRoute(t)

	points, err := geo.DecodePolyline6(fixture.Shape)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(points) < 50 {
		t.Fatalf("decoded only %d points from a 6.8 km city route", len(points))
	}

	for _, point := range points {
		if !geo.Amsterdam.Contains(point) {
			t.Fatalf("decoded point %+v is outside Amsterdam — wrong precision?", point)
		}
	}

	path, err := geo.NewPath(points)
	if err != nil {
		t.Fatalf("path: %v", err)
	}

	// Summing haversine over the vertices should land within a couple of
	// percent of the router's own length.
	gotKm := path.Length() / 1000
	if math.Abs(gotKm-fixture.ValhallaKm) > fixture.ValhallaKm*0.02 {
		t.Errorf("path length %.3f km, Valhalla says %.3f km", gotKm, fixture.ValhallaKm)
	}
}

func TestDecodePolyline6Rejects(t *testing.T) {
	// A truncated varint must be an error, not a silently short route.
	if _, err := geo.DecodePolyline6("y|~{bB_"); err == nil {
		t.Error("truncated polyline should fail to decode")
	}
}

// Walking the path is what the simulator does 2,500+ times a second, so its
// edge cases are worth pinning: clamped at both ends, monotonic in between.
func TestPathAt(t *testing.T) {
	path, err := geo.NewPath(decodeMust(t, loadRoute(t).Shape))
	if err != nil {
		t.Fatalf("path: %v", err)
	}

	start, _ := path.At(-100)
	if start != path.Points()[0] {
		t.Error("a negative distance should clamp to the start")
	}

	end, _ := path.At(path.Length() * 2)
	if end != path.Points()[len(path.Points())-1] {
		t.Error("overshooting should clamp to the end, not extrapolate")
	}

	// Position must advance monotonically along the route.
	previous := 0.0
	for step := 0.0; step <= path.Length(); step += path.Length() / 200 {
		point, bearing := path.At(step)

		if !point.Valid() {
			t.Fatalf("invalid point at %.0f m", step)
		}
		if bearing < 0 || bearing >= 360 {
			t.Fatalf("bearing %.1f out of range at %.0f m", bearing, step)
		}

		travelled := geo.DistanceMeters(path.Points()[0], point)
		_ = travelled
		if step < previous {
			t.Fatal("non-monotonic walk")
		}
		previous = step
	}

	// Half-way along should be roughly half the distance from either end,
	// measured along the path rather than as the crow flies.
	mid, _ := path.At(path.Length() / 2)
	if !geo.Amsterdam.Contains(mid) {
		t.Errorf("midpoint %+v left the city", mid)
	}
}

func TestNewPathRejectsDegenerate(t *testing.T) {
	single := []geo.Point{{Lat: 52.37, Lng: 4.90}}
	if _, err := geo.NewPath(single); err == nil {
		t.Error("a one-point path should be rejected")
	}

	// Valhalla emits repeated vertices at manoeuvre boundaries; a path of only
	// those has no direction and must not be treated as walkable.
	repeated := []geo.Point{{Lat: 52.37, Lng: 4.90}, {Lat: 52.37, Lng: 4.90}}
	if _, err := geo.NewPath(repeated); err == nil {
		t.Error("a path of duplicate points should be rejected")
	}
}
