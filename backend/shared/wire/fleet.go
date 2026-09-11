package wire

// The fleet feed: what the console draws.
//
// Two hops, and they are shaped differently on purpose. Each matcher partition
// publishes a FleetFrame to `fleet.frames` once a second — every driver it
// owns and the promises it holds — because a partition is the unit that has
// one owner, so nothing downstream has to reconcile two views of one driver.
// The gateway keeps the latest frame per partition and turns them into a
// FleetUpdate per watching console, shaped by what that console is looking at:
// cell counts when zoomed out, individual drivers only inside the viewport when
// zoomed in. Streaming every driver to every console at 1 Hz would be about
// half a megabyte a second each, which is how a dispatch screen freezes a tab.
//
// Every slice here is allocated, never nil. encoding/json writes a nil slice as
// `null`, and the TypeScript schema for an array rejects `null` — so a frame
// from an empty partition would fail to decode in the browser.

const (
	// TagFleetFrame marks a record on `fleet.frames`.
	TagFleetFrame = "FleetFrame"

	// TagFleetUpdate is the console's view, pushed once a second while watching.
	TagFleetUpdate = "FleetUpdate"
	// TagDriverPosition is the assigned driver's position, pushed once a second
	// to the rider following that trip.
	TagDriverPosition = "DriverPosition"

	// TagClientWatchFleet asks for fleet updates for a viewport. Ops only.
	TagClientWatchFleet = "ClientWatchFleet"
	// TagClientUnwatchFleet stops them.
	TagClientUnwatchFleet = "ClientUnwatchFleet"
	// TagClientFollowTrip asks for the assigned driver's position.
	TagClientFollowTrip = "ClientFollowTrip"
	// TagClientUnfollowTrip stops it.
	TagClientUnfollowTrip = "ClientUnfollowTrip"
)

// FleetFrame is one matcher partition's world, once a second.
type FleetFrame struct {
	Tag       string `json:"_tag"`
	Partition int32  `json:"partition"`
	// Instance is the matcher holding the partition, so the console can show
	// how the city is divided between them and watch it move in a rebalance.
	Instance string        `json:"instance"`
	AtMs     int64         `json:"atMs"`
	Drivers  []FleetDriver `json:"drivers"`
	Pending  int           `json:"pending"`
	Offers   int           `json:"offers"`
	// MatchLatenciesMs are the matches completed since the previous frame,
	// request to acceptance. Carried on the frame rather than scraped from
	// Prometheus so the console's latency chart comes from the same stream as
	// its map, with no second system in the path.
	MatchLatenciesMs []int64 `json:"matchLatenciesMs"`
	Abandoned        int     `json:"abandoned"`
	// Surge is every cell this partition owns that is priced above 1.0.
	Surge []CellSurge `json:"surge"`
	// Requests are the rides this partition was asked for since the previous
	// frame. The gateway shows them on riders' maps as the area each came
	// from and never the pickup; the exact point stops at the gateway, as
	// every driver's id and position on this topic does.
	Requests []FrameRequest `json:"requests"`
}

// FrameRequest is one ride a partition was asked for.
type FrameRequest struct {
	TripID string  `json:"tripId"`
	Lat    float64 `json:"lat"`
	Lng    float64 `json:"lng"`
	// AtMs is when the rider asked.
	AtMs int64 `json:"atMs"`
}

// FleetDriver is one driver as the console draws them.
type FleetDriver struct {
	ID      string       `json:"id"`
	Lat     float64      `json:"lat"`
	Lng     float64      `json:"lng"`
	Heading float64      `json:"heading"`
	Status  DriverStatus `json:"status"`
	// Cell is the resolution-7 shard cell, which is what cell mode counts by.
	Cell string `json:"cell"`
	// Reserved is a driver held for a trip, which counts as busy even while
	// their own status still says idle.
	Reserved bool `json:"reserved"`
}

// Viewport is what a console is looking at.
type Viewport struct {
	West  float64 `json:"west"`
	South float64 `json:"south"`
	East  float64 `json:"east"`
	North float64 `json:"north"`
	Zoom  float64 `json:"zoom"`
}

// FleetUpdate is one console's view of the fleet.
type FleetUpdate struct {
	AtMs int64 `json:"atMs"`
	// Mode says which of Cells and Drivers is populated: "cells" or "drivers".
	Mode    string        `json:"mode"`
	Cells   []FleetCell   `json:"cells"`
	Drivers []FleetDriver `json:"drivers"`
	Shards  []FleetShard  `json:"shards"`
	Stats   FleetStats    `json:"stats"`
}

// FleetCell is one resolution-7 cell, counted.
type FleetCell struct {
	Cell    string `json:"cell"`
	Drivers int    `json:"drivers"`
	Idle    int    `json:"idle"`
	// Multiplier is the cell's surge, 1.0 when it has none.
	Multiplier float64 `json:"multiplier"`
	// Boundary is the hexagon, as [lng, lat] pairs in order. Computed by the
	// gateway, once per cell, so the browser draws a polygon without needing
	// an H3 library of its own.
	Boundary [][2]float64 `json:"boundary"`
}

// FleetShard is one matcher partition's load.
type FleetShard struct {
	Partition int32  `json:"partition"`
	Instance  string `json:"instance"`
	Drivers   int    `json:"drivers"`
	Pending   int    `json:"pending"`
	Offers    int    `json:"offers"`
	// AgeMs is how old this partition's latest frame is. A partition whose age
	// keeps climbing is one nobody owns right now.
	AgeMs int64 `json:"ageMs"`
}

// FleetStats are the numbers above the map.
type FleetStats struct {
	Drivers            int     `json:"drivers"`
	Idle               int     `json:"idle"`
	Pending            int     `json:"pending"`
	Offers             int     `json:"offers"`
	MatchedPerSecond   float64 `json:"matchedPerSecond"`
	AbandonedPerSecond float64 `json:"abandonedPerSecond"`
	P50Ms              int64   `json:"p50Ms"`
	P95Ms              int64   `json:"p95Ms"`
	P99Ms              int64   `json:"p99Ms"`
	// MaxMultiplier is the highest surge anywhere, 1.0 when nowhere surges;
	// SurgingCells is how many cells are above 1.0.
	MaxMultiplier float64 `json:"maxMultiplier"`
	SurgingCells  int     `json:"surgingCells"`
}

// DriverPosition is where a rider's driver is.
type DriverPosition struct {
	TripID   string  `json:"tripId"`
	DriverID string  `json:"driverId"`
	Lat      float64 `json:"lat"`
	Lng      float64 `json:"lng"`
	Heading  float64 `json:"heading"`
	AtMs     int64   `json:"atMs"`
	// EtaSeconds is the predicted wait until the driver reaches the pickup:
	// 0 when there is none — before any pickup has been observed, or once the
	// rider is in the car.
	EtaSeconds float64 `json:"etaSeconds"`
}
