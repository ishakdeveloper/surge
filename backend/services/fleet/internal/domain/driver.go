// Package domain is the fleet's rules: who may be offered work, what a car
// must be, and what paperwork is outstanding. No transport, no storage, and
// no provider.
package domain

import (
	"errors"
	"time"
)

// Status is a driver's standing.
type Status string

const (
	// StatusOnboarding is a driver with something outstanding.
	StatusOnboarding Status = "onboarding"
	// StatusApproved is identity verified, a vehicle approved, and every
	// document valid today.
	StatusApproved Status = "approved"
	// StatusBlocked is a reviewer's decision, whatever the paperwork says.
	StatusBlocked Status = "blocked"
)

// IdentityStatus is how far the identity check has got.
type IdentityStatus string

const (
	IdentityUnstarted  IdentityStatus = "unstarted"
	IdentityPending    IdentityStatus = "pending"
	IdentityProcessing IdentityStatus = "processing"
	IdentityVerified   IdentityStatus = "verified"
	IdentityFailed     IdentityStatus = "failed"
)

var (
	ErrNotFound = errors.New("fleet: not found")
	// ErrNotAllowed is a request the caller's role cannot make.
	ErrNotAllowed = errors.New("fleet: not allowed")
	// ErrPlateTaken is a plate another driver already offers.
	ErrPlateTaken = errors.New("fleet: that plate is already registered")
	// ErrConflict is a write that lost a race and should be retried against
	// fresh state.
	ErrConflict = errors.New("fleet: changed concurrently")
)

// InvalidError is a request that cannot be met as made. Its reason is for the
// person who made it.
type InvalidError struct{ Reason string }

func (e *InvalidError) Error() string { return "fleet: " + e.Reason }

// Driver is a driver's standing and what the identity check found.
type Driver struct {
	ID     string
	Status Status
	// BlockedReason is why a reviewer stopped them, or why the identity check
	// refused them. Empty otherwise.
	BlockedReason string

	Identity          IdentityStatus
	IdentitySessionID string
	// VerifiedName and VerifiedDocument are what the provider read off the
	// licence. Empty until it passes.
	VerifiedName     string
	VerifiedDocument string
	LicenceExpiresAt time.Time

	ApprovedAt time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// RequirementKind is one thing standing between a driver and their first trip.
type RequirementKind string

const (
	NeedIdentity        RequirementKind = "identity"
	NeedVehicle         RequirementKind = "vehicle"
	NeedInsurance       RequirementKind = "insurance"
	NeedVOG             RequirementKind = "vog"
	NeedChauffeurskaart RequirementKind = "chauffeurskaart"
)

// Requirement is what to tell the driver to do next.
type Requirement struct {
	Kind   RequirementKind
	Detail string
}

// Papers is everything a driver's standing is worked out from.
type Papers struct {
	Driver    Driver
	Vehicles  []Vehicle
	Documents []Document
}

// Outstanding is what the driver still owes, in the order to ask for it.
//
// Derived from the papers every time rather than remembered, which is what
// keeps a driver from staying approved with a licence that lapsed in March:
// the licence's own expiry is one of the things checked here.
func Outstanding(papers Papers, now time.Time) []Requirement {
	var outstanding []Requirement

	switch {
	case papers.Driver.Identity != IdentityVerified:
		outstanding = append(outstanding, Requirement{
			Kind:   NeedIdentity,
			Detail: "Verify your identity with your driving licence.",
		})
	case !papers.Driver.LicenceExpiresAt.IsZero() && !now.Before(papers.Driver.LicenceExpiresAt):
		outstanding = append(outstanding, Requirement{
			Kind:   NeedIdentity,
			Detail: "Your driving licence has expired. Verify again with a current one.",
		})
	}

	vehicle, hasVehicle := UsableVehicle(papers.Vehicles, now)
	if !hasVehicle {
		outstanding = append(outstanding, Requirement{
			Kind:   NeedVehicle,
			Detail: "Add a car and have it approved.",
		})
	}

	// Insurance belongs to a car, so it is only asked for once there is one.
	if hasVehicle && !hasValid(papers.Documents, KindInsurance, vehicle.ID, now) {
		outstanding = append(outstanding, Requirement{
			Kind:   NeedInsurance,
			Detail: "Upload the insurance certificate for " + vehicle.Plate + ".",
		})
	}

	for _, owed := range []struct {
		kind RequirementKind
		doc  DocumentKind
		ask  string
	}{
		{NeedVOG, KindVOG, "Apply for a VOG and upload it when Justis sends it."},
		{NeedChauffeurskaart, KindChauffeurskaart, "Apply for a chauffeurskaart and upload it when Kiwa sends it."},
	} {
		if !hasValid(papers.Documents, owed.doc, "", now) {
			outstanding = append(outstanding, Requirement{Kind: owed.kind, Detail: owed.ask})
		}
	}

	return outstanding
}

// StandingOf is the status the papers support. A block is a person's decision
// and survives anything the papers say.
func StandingOf(papers Papers, now time.Time) Status {
	if papers.Driver.Status == StatusBlocked {
		return StatusBlocked
	}
	if len(Outstanding(papers, now)) == 0 {
		return StatusApproved
	}
	return StatusOnboarding
}

// hasValid reports whether a document of this kind is approved and unexpired.
// A vehicleID narrows it to the papers for one car.
func hasValid(documents []Document, kind DocumentKind, vehicleID string, now time.Time) bool {
	for _, document := range documents {
		if document.Kind != kind || !document.ValidAt(now) {
			continue
		}
		if vehicleID == "" || document.VehicleID == vehicleID {
			return true
		}
	}
	return false
}
