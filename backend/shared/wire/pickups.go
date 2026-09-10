package wire

// Pickups: how long it actually takes a car to reach its rider.
//
// Observed by ingest, which reads every driver's pings in order and so sees
// the two moments that bracket a pickup: the status turning to enroute_pickup
// — the driver accepted and set off — and then to on_trip, the rider in. They
// go to `pickups.observed`, keyed by the pickup's resolution-7 cell, where the
// gateway learns how long pickups take in each part of the city.

// TagPickupObserved marks a record on `pickups.observed`.
const TagPickupObserved = "PickupObserved"

// PickupObserved is one pickup the fleet made.
type PickupObserved struct {
	Tag      string `json:"_tag"`
	DriverID string `json:"driverId"`
	// Cell is the resolution-7 cell the driver arrived in: where the rider was.
	Cell string `json:"cell"`
	// FromLat and FromLng are where the driver set off; Lat and Lng where they
	// arrived.
	FromLat float64 `json:"fromLat"`
	FromLng float64 `json:"fromLng"`
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	// Meters is the straight-line distance between the two, and Seconds how
	// long the drive took by the driver's own clock.
	Meters  float64 `json:"meters"`
	Seconds float64 `json:"seconds"`
	AtMs    int64   `json:"atMs"`
}
