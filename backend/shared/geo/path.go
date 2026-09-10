package geo

import (
	"fmt"
	"sort"
	"strings"
)

// Path is a route polyline with its cumulative distances precomputed.
//
// This is the simulator's central primitive. A driver is not a position — it is
// a route plus a distance travelled along it, advanced by (speed x elapsed) on
// every tick. Precomputing the cumulative distances turns "where am I after
// 4 seconds" into a binary search rather than a walk from the start, which is
// the difference between 40,000 drivers costing O(n) and O(n log n) per tick.
type Path struct {
	points     []Point
	cumulative []float64
}

// NewPath builds a path from consecutive points. Duplicate consecutive points
// are dropped: Valhalla emits them at manoeuvre boundaries, and a zero-length
// segment would make the bearing at that vertex undefined.
func NewPath(points []Point) (*Path, error) {
	deduped := make([]Point, 0, len(points))
	for _, point := range points {
		if !point.Valid() {
			return nil, fmt.Errorf("geo: path contains invalid point %+v", point)
		}
		if len(deduped) > 0 && deduped[len(deduped)-1] == point {
			continue
		}
		deduped = append(deduped, point)
	}

	if len(deduped) < 2 {
		return nil, fmt.Errorf("geo: path needs at least 2 distinct points, got %d", len(deduped))
	}

	cumulative := make([]float64, len(deduped))
	for i := 1; i < len(deduped); i++ {
		cumulative[i] = cumulative[i-1] + DistanceMeters(deduped[i-1], deduped[i])
	}

	return &Path{points: deduped, cumulative: cumulative}, nil
}

// Length is the total distance along the path, in metres.
func (p *Path) Length() float64 { return p.cumulative[len(p.cumulative)-1] }

// Points returns the underlying vertices. Read-only by convention.
func (p *Path) Points() []Point { return p.points }

// At returns the position and heading after travelling `distance` metres.
//
// Distances before the start clamp to the first point and distances past the
// end clamp to the last, so a caller that overshoots gets "arrived" rather than
// an extrapolated position off the end of the road.
func (p *Path) At(distance float64) (Point, float64) {
	last := len(p.points) - 1

	if distance <= 0 {
		return p.points[0], BearingDegrees(p.points[0], p.points[1])
	}
	if distance >= p.Length() {
		return p.points[last], BearingDegrees(p.points[last-1], p.points[last])
	}

	// The first vertex at or past `distance`; the segment we are on ends there.
	index := sort.SearchFloat64s(p.cumulative, distance)
	if index == 0 {
		index = 1
	}

	from, to := p.points[index-1], p.points[index]
	segment := p.cumulative[index] - p.cumulative[index-1]

	var t float64
	if segment > 0 {
		t = (distance - p.cumulative[index-1]) / segment
	}

	return Lerp(from, to, t), BearingDegrees(from, to)
}

// DecodePolyline6 decodes an encoded polyline at precision 6.
//
// Precision 6, not Google's 5: Valhalla encodes its shapes at 1e-6 degrees, and
// decoding one at precision 5 yields a route that looks plausible and is off by
// a factor of ten — a bug that renders as a driver in Belgium.
func DecodePolyline6(encoded string) ([]Point, error) {
	return decodePolyline(encoded, 1e6)
}

func decodePolyline(encoded string, factor float64) ([]Point, error) {
	var (
		points []Point
		lat    int64
		lng    int64
		index  int
	)

	next := func() (int64, error) {
		var (
			result int64
			shift  uint
		)
		for {
			if index >= len(encoded) {
				return 0, fmt.Errorf("geo: polyline truncated at %d", index)
			}
			b := int64(encoded[index]) - 63
			index++
			result |= (b & 0x1f) << shift
			if b < 0x20 {
				break
			}
			shift += 5
			if shift > 63 {
				return 0, fmt.Errorf("geo: polyline value overflows at %d", index)
			}
		}
		// Zig-zag: the low bit is the sign.
		if result&1 != 0 {
			return ^(result >> 1), nil
		}
		return result >> 1, nil
	}

	for index < len(encoded) {
		dLat, err := next()
		if err != nil {
			return nil, err
		}
		dLng, err := next()
		if err != nil {
			return nil, err
		}

		lat += dLat
		lng += dLng

		points = append(points, Point{
			Lat: float64(lat) / factor,
			Lng: float64(lng) / factor,
		})
	}

	if len(points) == 0 && strings.TrimSpace(encoded) != "" {
		return nil, fmt.Errorf("geo: polyline decoded to nothing")
	}

	return points, nil
}
