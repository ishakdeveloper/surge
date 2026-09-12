package wire

// Facts on `fleet.drivers`: who may be offered work, and in what.
//
// Keyed by driver id on a compacted topic, so a consumer starting cold reads
// the current standing of every driver rather than replaying every decision
// ever made about them. The matcher is the consumer this exists for: a driver
// whose insurance lapsed this morning should stop being offered rides without
// anybody telling the matcher twice.
const (
	// FactDriverApproved is a driver who may be offered work: identity
	// verified, a vehicle approved, every document valid.
	FactDriverApproved = "DriverApproved"
	// FactDriverWithdrawn is a driver who may not, whether because a paper
	// lapsed or because a reviewer stopped them.
	FactDriverWithdrawn = "DriverWithdrawn"
)

// FleetFact is the envelope on `fleet.drivers`, keyed by driver id.
//
// Flat, and carrying the whole standing rather than what changed: a consumer
// that missed the approval and sees only the withdrawal still knows everything
// it needs, which is the property compaction depends on.
type FleetFact struct {
	Tag      string `json:"_tag"`
	DriverID string `json:"driverId"`
	// Plate and PackageSlug are the car they would be dispatched in, and are
	// empty for a withdrawn driver.
	Plate       string `json:"plate"`
	PackageSlug string `json:"packageSlug"`
	// Reason is why a withdrawn driver was withdrawn, and empty otherwise.
	Reason string `json:"reason"`
	AtMs   int64  `json:"atMs"`
}

// Valid rejects a fact no consumer could act on.
func (f FleetFact) Valid() bool {
	if f.DriverID == "" {
		return false
	}
	switch f.Tag {
	case FactDriverApproved:
		// An approved driver drives something, or there is nothing to dispatch.
		return f.Plate != "" && f.PackageSlug != ""
	case FactDriverWithdrawn:
		return true
	default:
		return false
	}
}
