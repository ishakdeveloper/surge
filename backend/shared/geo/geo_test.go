package geo_test

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/ishakdeveloper/surge/shared/geo"
)

var (
	centraal    = geo.Point{Lat: 52.3791, Lng: 4.9003}
	rijksmuseum = geo.Point{Lat: 52.3600, Lng: 4.8852}
)

func TestDistanceMeters(t *testing.T) {
	// Straight-line distance between the two, ~2.4 km. The driving route is
	// 6.84 km, and the gap between those two numbers is the whole reason the
	// matcher asks Valhalla for a time rather than trusting haversine.
	got := geo.DistanceMeters(centraal, rijksmuseum)

	if math.Abs(got-2400) > 150 {
		t.Errorf("got %.0f m, want ~2400 m", got)
	}

	if d := geo.DistanceMeters(centraal, centraal); d != 0 {
		t.Errorf("distance to self should be 0, got %v", d)
	}

	// Symmetry, because an asymmetric distance would make ring searches depend
	// on which end you started from.
	if a, b := geo.DistanceMeters(centraal, rijksmuseum), geo.DistanceMeters(rijksmuseum, centraal); math.Abs(a-b) > 1e-9 {
		t.Errorf("asymmetric: %v vs %v", a, b)
	}
}

func TestBearingDegrees(t *testing.T) {
	origin := geo.Point{Lat: 52.37, Lng: 4.90}

	tests := []struct {
		name string
		to   geo.Point
		want float64
	}{
		{"due north", geo.Point{Lat: 52.38, Lng: 4.90}, 0},
		{"due east", geo.Point{Lat: 52.37, Lng: 4.91}, 90},
		{"due south", geo.Point{Lat: 52.36, Lng: 4.90}, 180},
		{"due west", geo.Point{Lat: 52.37, Lng: 4.89}, 270},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := geo.BearingDegrees(origin, test.to)
			// Bearings wrap, so compare the shortest angular difference.
			diff := math.Mod(math.Abs(got-test.want), 360)
			if diff > 180 {
				diff = 360 - diff
			}
			if diff > 1 {
				t.Errorf("got %.1f°, want %.1f°", got, test.want)
			}
		})
	}
}

// The property the whole sharding design rests on: a point has exactly one
// shard cell, and finer cells roll up to it. If this were ever false, two
// matcher instances could both believe they own the same driver.
func TestShardOwnershipIsUnambiguous(t *testing.T) {
	source := rand.New(rand.NewPCG(1, 2))

	for range 500 {
		point := geo.Amsterdam.Random(source)

		shard, err := geo.ShardCell(point)
		if err != nil {
			t.Fatalf("shard cell: %v", err)
		}
		index, err := geo.IndexCell(point)
		if err != nil {
			t.Fatalf("index cell: %v", err)
		}

		if shard.Resolution() != geo.ShardRes {
			t.Fatalf("shard resolution %d, want %d", shard.Resolution(), geo.ShardRes)
		}
		if index.Resolution() != geo.IndexRes {
			t.Fatalf("index resolution %d, want %d", index.Resolution(), geo.IndexRes)
		}

		// Routing a reservation to the owner depends on this exact rollup.
		parent, err := geo.ShardOf(index)
		if err != nil {
			t.Fatalf("shard of index cell: %v", err)
		}
		if parent != shard {
			t.Fatalf("index cell %s rolls up to %s, but the point's shard is %s", index, parent, shard)
		}
	}
}

// Amsterdam has to hold enough shard cells for ownership to spread across
// matcher instances. Too few and a rebalance moves the entire city.
func TestAmsterdamHasEnoughShardCells(t *testing.T) {
	source := rand.New(rand.NewPCG(7, 7))
	seen := map[geo.Cell]struct{}{}

	for range 20000 {
		cell, err := geo.ShardCell(geo.Amsterdam.Random(source))
		if err != nil {
			t.Fatalf("shard cell: %v", err)
		}
		seen[cell] = struct{}{}
	}

	if len(seen) < 60 {
		t.Errorf("only %d shard cells cover the simulator's world; too few to spread over matcher instances", len(seen))
	}
	t.Logf("Amsterdam covers %d shard cells at resolution %d", len(seen), geo.ShardRes)
}

func TestRing(t *testing.T) {
	cell, err := geo.ShardCell(centraal)
	if err != nil {
		t.Fatalf("shard cell: %v", err)
	}

	// A hexagon has six neighbours, so k=1 is 7 cells and k=2 is 19. This is
	// the property geohash rectangles do not have, and the reason for H3.
	for k, want := range map[int]int{0: 1, 1: 7, 2: 19} {
		ring, err := geo.Ring(cell, k)
		if err != nil {
			t.Fatalf("ring k=%d: %v", k, err)
		}
		if len(ring) != want {
			t.Errorf("ring k=%d has %d cells, want %d", k, len(ring), want)
		}
	}
}

func TestPointValidation(t *testing.T) {
	for _, point := range []geo.Point{
		{Lat: math.NaN(), Lng: 4.9},
		{Lat: 52.3, Lng: math.NaN()},
		{Lat: 91, Lng: 4.9},
		{Lat: 52.3, Lng: 181},
	} {
		if point.Valid() {
			t.Errorf("%+v should be invalid", point)
		}
		if _, err := geo.ShardCell(point); err == nil {
			t.Errorf("%+v should not produce a cell", point)
		}
	}
}

// DecodePolyline6Must is a test helper: a fixture that will not decode is a
// broken fixture, not a test failure worth threading an error through.
func decodeMust(t *testing.T, shape string) []geo.Point {
	t.Helper()

	points, err := geo.DecodePolyline6(shape)
	if err != nil {
		t.Fatalf("fixture polyline does not decode: %v", err)
	}
	return points
}
