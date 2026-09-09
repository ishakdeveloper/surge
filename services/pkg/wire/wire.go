// Package wire is the cross-service message vocabulary.
//
// TODO(phase 4): these types become generated output. `packages/domain` is the
// contract both languages compile against, and `scripts/schemagen` will walk
// the Effect Schema definitions and emit this file, with golden JSON fixtures
// holding the two sides honest in both directions. They are hand-written for
// now because nothing in TypeScript consumes them yet, and they are shaped the
// way the generator will emit them so that switch is a diff rather than a
// rewrite.
//
// The reason this matters is on display in the starter this project borrows
// from: its `web/src/contracts.ts` and `shared/contracts/amqp.go` are a
// hand-maintained mirror of one another, and they have already drifted — the
// TypeScript declares trip events the Go does not have, and the Go declares a
// whole payment command set the TypeScript does not.
//
// Encoding is JSON, deliberately, for now. It costs more than a binary format
// at 10k messages a second, and being able to read a partition in Redpanda
// Console while debugging a rebalance is worth more than that during
// development. Swapping the hot topic to a compact encoding is a measurable
// experiment for later, not an assumption to bake in now.
package wire

import "github.com/ishakdeveloper/surge/pkg/geo"

// DriverStatus is where a driver is in the trip lifecycle.
type DriverStatus string

const (
	// StatusOffline means not accepting work. Still emits pings while the app
	// is open, because the map shows supply, not just availability.
	StatusOffline DriverStatus = "offline"
	// StatusIdle is available and cruising — the pool the matcher draws from.
	StatusIdle DriverStatus = "idle"
	// StatusEnRoutePickup is assigned and driving to the rider.
	StatusEnRoutePickup DriverStatus = "enroute_pickup"
	// StatusOnTrip is carrying a rider.
	StatusOnTrip DriverStatus = "on_trip"
)

func (s DriverStatus) Valid() bool {
	switch s {
	case StatusOffline, StatusIdle, StatusEnRoutePickup, StatusOnTrip:
		return true
	default:
		return false
	}
}

// Available reports whether the matcher may offer this driver a ride.
func (s DriverStatus) Available() bool { return s == StatusIdle }

// DriverPing is one GPS report. This is the highest-volume message in the
// system by two orders of magnitude, so its shape is worth being deliberate
// about.
type DriverPing struct {
	DriverID string `json:"driverId"`

	// Seq is a per-driver monotonic counter.
	//
	// Load-bearing, not decoration. When a driver crosses a cell boundary the
	// ingest service emits DriverLeftCell to one Kafka partition and
	// DriverEnteredCell to another, and Kafka orders records within a partition
	// but not across them. The two can therefore arrive at their shards in
	// either order. Seq is what lets a shard recognise and drop an update older
	// than the one it already has, instead of resurrecting a driver who has
	// already left.
	Seq uint64 `json:"seq"`

	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`

	// Heading in degrees clockwise from north, so the map can point the marker.
	Heading float64 `json:"heading"`
	// SpeedMps is metres per second.
	SpeedMps float64 `json:"speedMps"`

	Status DriverStatus `json:"status"`

	// SentAtMs is when the client emitted this, in Unix milliseconds.
	//
	// The end-to-end latency histogram is (received - SentAtMs), which is the
	// number that actually says whether ingest is keeping up. Consumer lag says
	// how many records are outstanding; this says how stale the map is, and
	// they diverge exactly when it matters.
	SentAtMs int64 `json:"sentAtMs"`
}

// Point is the driver's position as the geo package sees it.
func (p DriverPing) Point() geo.Point { return geo.Point{Lat: p.Lat, Lng: p.Lng} }

// Valid rejects a ping that cannot be indexed. An invalid ping is dropped at
// ingest rather than being allowed to poison a shard's index.
func (p DriverPing) Valid() bool {
	return p.DriverID != "" && p.Status.Valid() && p.Point().Valid()
}
