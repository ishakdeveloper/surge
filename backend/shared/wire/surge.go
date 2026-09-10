package wire

// Surge: what a cell costs right now, and why.
//
// Computed by the matcher shard that owns the cell — the only process that sees
// both its waiting riders and its idle cars — and published two ways: on the
// shard's FleetFrame, for the console's heat layer, and to the compacted
// `surge.cells` topic keyed by cell, where the trip service reads the latest
// multiplier for a pickup when it prices a quote.

// TagCellSurge marks a record on `surge.cells`.
const TagCellSurge = "CellSurge"

// CellSurge is one resolution-7 cell's price multiplier.
type CellSurge struct {
	Tag  string `json:"_tag"`
	Cell string `json:"cell"`
	// Multiplier is at least 1.0, in steps of 0.1: a price that twitches every
	// second is a price riders stop trusting.
	Multiplier float64 `json:"multiplier"`
	// Demand and Supply are the smoothed counts it came from — riders waiting
	// and idle cars in the cell, averaged over the surge window.
	Demand float64 `json:"demand"`
	Supply float64 `json:"supply"`
	AtMs   int64   `json:"atMs"`
}
