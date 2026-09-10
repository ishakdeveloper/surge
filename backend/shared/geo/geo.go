// Package geo is the spatial vocabulary the whole system shares: H3 cells,
// distance on the sphere, and walking a route polyline.
//
// Two resolutions, and the difference between them is the architecture.
//
// Resolution 7 (~5.16 km², ~1.2 km edge) is the SHARD cell. It is the Kafka
// message key, so it is also the unit of ownership: one matcher instance owns a
// partition, a partition owns a set of res-7 cells, and that instance is the
// only writer for every driver standing in them. The single-writer property
// that removes the need for a distributed lock is this constant.
//
// Resolution 9 (~0.105 km², ~174 m edge) is the INDEX cell — the bucket a
// driver's position lands in inside a shard's in-memory index. At 10k drivers
// over Amsterdam that is roughly two drivers per cell, which is the right grain
// for expanding a search ring outward.
//
// Hexagons rather than the geohash rectangles the starter used: every neighbour
// of a hexagon shares an edge and sits at the same distance, so `GridDisk(k)`
// is a genuine "search radius k" rather than a box whose corners are 1.4x
// further away than its sides.
package geo

import (
	"fmt"
	"math"

	"github.com/uber/h3-go/v4"
)

const (
	// ShardRes is the resolution of a matcher shard cell, and of the Kafka key.
	ShardRes = 7
	// IndexRes is the resolution of the driver position index inside a shard.
	IndexRes = 9
)

// earthRadiusMeters is the IUGG mean radius.
const earthRadiusMeters = 6371008.8

// Point is a WGS84 coordinate. Lng, not Lon: Valhalla and H3 both say Lng, and
// one spelling everywhere is worth more than any argument about which.
type Point struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

func (p Point) latLng() h3.LatLng { return h3.LatLng{Lat: p.Lat, Lng: p.Lng} }

// Valid rejects coordinates outside the sphere and the NaNs that a bad
// interpolation produces — cheap here, and the alternative is an H3 error
// surfacing three layers away from the arithmetic that caused it.
func (p Point) Valid() bool {
	return !math.IsNaN(p.Lat) && !math.IsNaN(p.Lng) &&
		p.Lat >= -90 && p.Lat <= 90 && p.Lng >= -180 && p.Lng <= 180
}

// Cell is an H3 index at some resolution.
type Cell = h3.Cell

// CellAt returns the cell containing p at the given resolution.
func CellAt(p Point, resolution int) (Cell, error) {
	if !p.Valid() {
		return 0, fmt.Errorf("geo: invalid point %+v", p)
	}

	cell, err := h3.LatLngToCell(p.latLng(), resolution)
	if err != nil {
		return 0, fmt.Errorf("geo: cell at res %d: %w", resolution, err)
	}
	return cell, nil
}

// IndexCell is the index bucket for a position, and the canonical cell: every
// other cell for that position is derived from it.
func IndexCell(p Point) (Cell, error) { return CellAt(p, IndexRes) }

// ShardCell is the owning shard for a position.
//
// Derived from the index cell rather than computed directly, and that is not a
// stylistic choice — it is a correctness one.
//
// H3 is NOT hierarchically consistent under LatLngToCell. Hexagons do not tile
// into larger hexagons, so a resolution-9 cell is not wholly contained by any
// resolution-7 cell, and near a boundary `LatLngToCell(p, 7)` and
// `LatLngToCell(p, 9).Parent(7)` return different cells for the same point.
// Both answers are defensible; having two of them is not.
//
// In this system a cell is an ownership claim: it is the Kafka key, so it
// decides which matcher instance is the single writer for a driver. Two code
// paths disagreeing about a driver's shard means two instances each believing
// they own it, which is precisely the race the single-writer design exists to
// make impossible — and it would only ever appear for drivers near a cell edge,
// under load, as an occasional double-dispatch.
//
// So there is one path to a shard cell, and it goes through the index cell.
// TestShardOwnershipIsUnambiguous is what holds that true.
func ShardCell(p Point) (Cell, error) {
	index, err := IndexCell(p)
	if err != nil {
		return 0, err
	}
	return ShardOf(index)
}

// ShardKey is the Kafka message key for a position.
//
// A string rather than the int64, because it is a partition key: franz-go
// hashes the bytes, and the hex form is also what shows up in Redpanda Console
// when you are staring at a partition wondering which part of Amsterdam it is.
func ShardKey(p Point) (string, error) {
	cell, err := ShardCell(p)
	if err != nil {
		return "", err
	}
	return cell.String(), nil
}

// ShardOf returns the res-7 ancestor of any finer cell. Used to route a
// reservation to the shard that owns a driver's current index cell.
func ShardOf(cell Cell) (Cell, error) {
	parent, err := cell.Parent(ShardRes)
	if err != nil {
		return 0, fmt.Errorf("geo: shard of %s: %w", cell, err)
	}
	return parent, nil
}

// Ring returns cell and everything within k steps of it, cell first.
func Ring(cell Cell, k int) ([]Cell, error) {
	cells, err := h3.GridDisk(cell, k)
	if err != nil {
		return nil, fmt.Errorf("geo: ring k=%d around %s: %w", k, cell, err)
	}
	return cells, nil
}

// DistanceMeters is the great-circle distance between two points.
//
// Haversine, not Vincenty: at city scale the ellipsoidal correction is under a
// metre, and this runs once per candidate driver per match.
func DistanceMeters(a, b Point) float64 {
	lat1 := a.Lat * math.Pi / 180
	lat2 := b.Lat * math.Pi / 180
	dLat := lat2 - lat1
	dLng := (b.Lng - a.Lng) * math.Pi / 180

	sinLat := math.Sin(dLat / 2)
	sinLng := math.Sin(dLng / 2)

	h := sinLat*sinLat + math.Cos(lat1)*math.Cos(lat2)*sinLng*sinLng

	return 2 * earthRadiusMeters * math.Asin(math.Sqrt(math.Min(1, h)))
}

// BearingDegrees is the initial compass bearing from a to b, 0-360.
// The simulator reports it as a driver's heading, which is what lets the map
// point the car markers the right way.
func BearingDegrees(a, b Point) float64 {
	lat1 := a.Lat * math.Pi / 180
	lat2 := b.Lat * math.Pi / 180
	dLng := (b.Lng - a.Lng) * math.Pi / 180

	y := math.Sin(dLng) * math.Cos(lat2)
	x := math.Cos(lat1)*math.Sin(lat2) - math.Sin(lat1)*math.Cos(lat2)*math.Cos(dLng)

	degrees := math.Atan2(y, x) * 180 / math.Pi

	return math.Mod(degrees+360, 360)
}

// Lerp interpolates between a and b. Linear in lat/lng, which is wrong on a
// sphere and irrelevant over the tens of metres a driver covers between two
// GPS pings.
func Lerp(a, b Point, t float64) Point {
	return Point{
		Lat: a.Lat + (b.Lat-a.Lat)*t,
		Lng: a.Lng + (b.Lng-a.Lng)*t,
	}
}

// Boundary is a cell's outline, for drawing it: vertices in order, not closed.
func Boundary(cell string) ([]Point, error) {
	c := h3.CellFromString(cell)
	if !c.IsValid() {
		return nil, fmt.Errorf("geo: %q is not an H3 cell", cell)
	}
	vertices, err := c.Boundary()
	if err != nil {
		return nil, fmt.Errorf("geo: boundary of %s: %w", cell, err)
	}
	points := make([]Point, len(vertices))
	for i, vertex := range vertices {
		points[i] = Point{Lat: vertex.Lat, Lng: vertex.Lng}
	}
	return points, nil
}
