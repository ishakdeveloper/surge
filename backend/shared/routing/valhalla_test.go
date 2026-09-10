package routing_test

import (
	"context"
	"errors"
	"math/rand/v2"
	"os"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/shared/geo"
	"github.com/ishakdeveloper/surge/shared/routing"
)

func client(t *testing.T) *routing.Client {
	t.Helper()

	url := os.Getenv("VALHALLA_URL")
	if url == "" {
		url = "http://localhost:8002"
	}

	c := routing.New(url)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.Healthy(ctx); err != nil {
		t.Skipf("valhalla not available: %v", err)
	}
	return c
}

var (
	centraal    = geo.Point{Lat: 52.3791, Lng: 4.9003}
	rijksmuseum = geo.Point{Lat: 52.3600, Lng: 4.8852}
)

func TestRoute(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	route, err := client(t).Route(ctx, centraal, rijksmuseum)
	if err != nil {
		t.Fatalf("route: %v", err)
	}

	if route.Duration <= 0 || route.Meters <= 0 {
		t.Fatalf("degenerate route: %v / %.0f m", route.Duration, route.Meters)
	}

	// The driving route is meaningfully longer than the straight line — canals
	// and one-way streets. If these ever matched, we would be routing over
	// something other than roads.
	straight := geo.DistanceMeters(centraal, rijksmuseum)
	if route.Meters < straight*1.2 {
		t.Errorf("route %.0f m is suspiciously close to the %.0f m straight line", route.Meters, straight)
	}

	// The decoded polyline should agree with the summary Valhalla reported.
	if diff := route.Path.Length() - route.Meters; diff > route.Meters*0.02 || diff < -route.Meters*0.02 {
		t.Errorf("path is %.0f m but summary says %.0f m", route.Path.Length(), route.Meters)
	}
}

// The simulator drops points into a bounding box that includes open water, so
// "no route" is a normal outcome it has to distinguish from a real failure.
func TestUnreachablePointIsATypedOutcome(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Middle of the North Sea.
	_, err := client(t).Route(ctx, centraal, geo.Point{Lat: 54.0, Lng: 3.0})
	if err == nil {
		t.Fatal("routing into the North Sea should not succeed")
	}

	var noRoute *routing.NoRouteError
	if !errors.As(err, &noRoute) {
		t.Fatalf("want *NoRouteError so the simulator can retry, got %T: %v", err, err)
	}
}

// The matrix is what makes batched assignment cheaper than N greedy lookups.
func TestMatrix(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	source := rand.New(rand.NewPCG(11, 13))
	drivers := []geo.Point{centraal, rijksmuseum, geo.Amsterdam.Center()}
	riders := []geo.Point{
		{Lat: 52.3676, Lng: 4.9041},
		{Lat: 52.3545, Lng: 4.8700},
		geo.Amsterdam.Random(source),
	}

	matrix, err := client(t).Matrix(ctx, drivers, riders)
	if err != nil {
		t.Fatalf("matrix: %v", err)
	}

	if len(matrix) != len(drivers) {
		t.Fatalf("got %d rows, want %d", len(matrix), len(drivers))
	}

	reachable := 0
	for i, row := range matrix {
		if len(row) != len(riders) {
			t.Fatalf("row %d has %d columns, want %d", i, len(row), len(riders))
		}
		for _, cell := range row {
			if cell.Reachable {
				reachable++
				if cell.Duration <= 0 {
					t.Errorf("reachable cell with non-positive duration: %+v", cell)
				}
			}
		}
	}

	// A 3x3 over central Amsterdam should be almost entirely reachable; if it
	// is not, the tiles are wrong rather than the code.
	if reachable < 6 {
		t.Errorf("only %d of 9 pairs reachable", reachable)
	}
}

func TestMatrixRejectsEmpty(t *testing.T) {
	ctx := context.Background()
	if _, err := routing.New("http://localhost:8002").Matrix(ctx, nil, []geo.Point{centraal}); err == nil {
		t.Error("an empty source list should be rejected before hitting the network")
	}
}
