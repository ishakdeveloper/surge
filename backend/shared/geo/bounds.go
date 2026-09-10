package geo

import "math/rand/v2"

// Bounds is an axis-aligned lat/lng box.
type Bounds struct {
	MinLat, MinLng, MaxLat, MaxLng float64
}

// Amsterdam is the simulator's world.
//
// Roughly 19 x 24 km, which is about 450 km² — some 88 shard cells at
// resolution 7. That number matters: it has to comfortably exceed the number of
// matcher instances so ownership can actually spread, and comfortably exceed
// the partition count's granularity so a rebalance moves a meaningful slice of
// the map rather than all of it.
//
// The box includes water. Points landing in the IJ or the IJmeer simply fail to
// route, and the simulator retries — cheaper than carrying a city polygon, and
// it produces a realistic clustering along the roads that do exist.
var Amsterdam = Bounds{
	MinLat: 52.28, MinLng: 4.70,
	MaxLat: 52.45, MaxLng: 5.05,
}

func (b Bounds) Contains(p Point) bool {
	return p.Lat >= b.MinLat && p.Lat <= b.MaxLat && p.Lng >= b.MinLng && p.Lng <= b.MaxLng
}

// Random returns a uniformly distributed point inside the box.
//
// Takes the generator rather than using the global one, so a simulator run with
// a fixed seed is reproducible — which is what makes a matching benchmark
// comparable between two strategies instead of merely suggestive.
func (b Bounds) Random(r *rand.Rand) Point {
	return Point{
		Lat: b.MinLat + r.Float64()*(b.MaxLat-b.MinLat),
		Lng: b.MinLng + r.Float64()*(b.MaxLng-b.MinLng),
	}
}

// Center is the middle of the box.
func (b Bounds) Center() Point {
	return Point{Lat: (b.MinLat + b.MaxLat) / 2, Lng: (b.MinLng + b.MaxLng) / 2}
}
