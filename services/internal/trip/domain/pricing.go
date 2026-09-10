package domain

import (
	"fmt"
	"time"
)

// Package is a vehicle class and what it costs.
//
// Operator configuration expressed as data. A new class is a row here, not a
// protocol change — which is why `package_slug` is a string on the wire rather
// than an enum.
type Package struct {
	Slug           string
	Label          string
	Seats          int32
	BaseCents      int64
	PerKmCents     int64
	PerMinuteCents int64
	// MinimumCents is the floor. Without it a 400 metre trip prices below what
	// it costs a driver to show up.
	MinimumCents int64
}

// Catalogue is the offering. Amsterdam prices, roughly plausible.
var Catalogue = []Package{
	{Slug: "sedan", Label: "Surge", Seats: 4, BaseCents: 300, PerKmCents: 145, PerMinuteCents: 28, MinimumCents: 700},
	{Slug: "van", Label: "Surge XL", Seats: 6, BaseCents: 450, PerKmCents: 195, PerMinuteCents: 36, MinimumCents: 1000},
	{Slug: "luxury", Label: "Surge Black", Seats: 4, BaseCents: 800, PerKmCents: 310, PerMinuteCents: 55, MinimumCents: 1800},
}

func PackageBySlug(slug string) (Package, error) {
	for _, class := range Catalogue {
		if class.Slug == slug {
			return class, nil
		}
	}
	return Package{}, fmt.Errorf("trip: unknown package %q", slug)
}

// Fare is a quote.
type Fare struct {
	ID              string
	PackageSlug     string
	TotalCents      int64
	SurgeMultiplier float64
	ExpiresAt       time.Time
	// The route the quote was computed from, kept so accepting a fare does not
	// re-route and quietly produce a different price.
	Polyline6 string
	Meters    float64
	Seconds   int64
	Pickup    Coordinate
	Dropoff   Coordinate
}

// Coordinate mirrors geo.Point without importing it, so the domain does not
// depend on the H3 package for what is a pair of floats.
type Coordinate struct {
	Lat float64
	Lng float64
}

// Quote prices a route.
//
// Distance and time both, because either alone is wrong in a city: pure
// distance undercharges an hour in traffic on the A10, pure time overcharges a
// clear run through the tunnel.
func (p Package) Quote(meters float64, seconds int64, surge float64) int64 {
	if surge < 1 {
		surge = 1
	}

	base := p.BaseCents +
		int64(meters/1000*float64(p.PerKmCents)) +
		int64(float64(seconds)/60*float64(p.PerMinuteCents))

	// Surge multiplies the metered fare. The minimum is then a floor under the
	// result, and is never itself multiplied — so a short trip in a busy cell
	// still rises with demand, but a 200 metre hop at 3x cannot be priced at
	// three times the minimum for a journey that barely happened.
	surged := int64(float64(base) * surge)

	if surged < p.MinimumCents {
		return p.MinimumCents
	}
	return surged
}
