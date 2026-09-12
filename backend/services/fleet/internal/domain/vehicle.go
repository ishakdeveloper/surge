package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

// VehicleStatus is what was decided about a car.
type VehicleStatus string

const (
	VehiclePending  VehicleStatus = "pending"
	VehicleApproved VehicleStatus = "approved"
	VehicleRejected VehicleStatus = "rejected"
	VehicleRetired  VehicleStatus = "retired"
)

// Vehicle is a car a driver offers, as the register described it when we
// asked.
//
// The details are copied from the register rather than typed, and the copy
// carries the date it was taken: an APK that lapses next month has to stop the
// car being offered work without anybody retyping anything.
type Vehicle struct {
	ID          string
	DriverID    string
	Plate       string
	Make        string
	Model       string
	Colour      string
	Seats       int
	PackageSlug string

	Status         VehicleStatus
	RejectedReason string

	APKExpiresAt      time.Time
	TaxiRegistered    bool
	Insured           bool
	FirstRegisteredAt time.Time
	RegisterCheckedAt time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Registration is what the vehicle register says about a plate.
type Registration struct {
	Plate             string
	Make              string
	Model             string
	Colour            string
	Seats             int
	APKExpiresAt      time.Time
	TaxiRegistered    bool
	Insured           bool
	FirstRegisteredAt time.Time
}

// PlateLength is the number of characters in a Dutch plate, with the dashes
// taken out.
const PlateLength = 6

// NormalizePlate is a plate as the register holds it: upper case, no dashes,
// no spaces.
//
// Typed by a person on a phone, so the punctuation is thrown away rather than
// rejected: 02-JLT-3, 02 jlt 3 and 02JLT3 are one car.
func NormalizePlate(raw string) (string, error) {
	var plate strings.Builder
	for _, character := range raw {
		switch {
		case character == '-' || unicode.IsSpace(character):
		case unicode.IsLetter(character) || unicode.IsDigit(character):
			plate.WriteRune(unicode.ToUpper(character))
		default:
			return "", &InvalidError{Reason: "a plate is letters and digits, with or without dashes"}
		}
	}
	if plate.Len() != PlateLength {
		return "", &InvalidError{Reason: fmt.Sprintf("a Dutch plate has %d characters", PlateLength)}
	}
	return plate.String(), nil
}

// Class is a ride class a car can be offered for.
//
// A copy of the slugs and seat counts in the trip service's catalogue, because
// a service may not reach into another's internals. Small, and checked against
// the real thing by a test, so the two cannot drift in silence.
type Class struct {
	Slug  string
	Seats int
}

// Classes is what a driver may offer a car for: Surge, Surge XL and Surge
// Black, by the slugs the trip service prices them under.
var Classes = []Class{
	{Slug: "sedan", Seats: 4},
	{Slug: "van", Seats: 6},
	{Slug: "luxury", Seats: 4},
}

// ClassBySlug finds a ride class.
func ClassBySlug(slug string) (Class, bool) {
	for _, class := range Classes {
		if class.Slug == slug {
			return class, true
		}
	}
	return Class{}, false
}

// CheckRegistration is whether a car may be offered at all, and what a
// reviewer should be told about it.
//
// Refusals are the facts nobody can argue with — the register has no such
// plate, the APK has lapsed, nobody insures it, it has too few seats for the
// class. Notes are for the reviewer to weigh: a car with no taxi registration
// is not yet a taxi, which is a conversation rather than a rejection.
func CheckRegistration(registration Registration, class Class, now time.Time) (notes []string, err error) {
	switch {
	case registration.APKExpiresAt.IsZero():
		return nil, &InvalidError{Reason: "the register has no inspection date for that plate"}
	case !now.Before(registration.APKExpiresAt):
		return nil, &InvalidError{
			Reason: "that car's APK expired on " + registration.APKExpiresAt.Format("2 January 2006"),
		}
	case !registration.Insured:
		return nil, &InvalidError{Reason: "the register has no insurance against that plate"}
	case registration.Seats < class.Seats:
		return nil, &InvalidError{
			Reason: fmt.Sprintf("%s needs %d seats and that car has %d", class.Slug, class.Seats, registration.Seats),
		}
	}

	if !registration.TaxiRegistered {
		notes = append(notes, "The register does not have this car down for taxi use.")
	}
	if soon := now.AddDate(0, 1, 0); registration.APKExpiresAt.Before(soon) {
		notes = append(notes, "Its APK expires on "+registration.APKExpiresAt.Format("2 January 2006")+".")
	}
	return notes, nil
}

// NewVehicle is a car as first registered with us, from what the register said.
func NewVehicle(id, driverID string, registration Registration, class Class, now time.Time) Vehicle {
	return Vehicle{
		ID: id, DriverID: driverID,
		Plate: registration.Plate, Make: registration.Make, Model: registration.Model,
		Colour: registration.Colour, Seats: registration.Seats, PackageSlug: class.Slug,
		Status:            VehiclePending,
		APKExpiresAt:      registration.APKExpiresAt,
		TaxiRegistered:    registration.TaxiRegistered,
		Insured:           registration.Insured,
		FirstRegisteredAt: registration.FirstRegisteredAt,
		RegisterCheckedAt: now,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
}

// UsableVehicle is the car a driver would be dispatched in: approved, and
// whose inspection has not lapsed since it was approved.
func UsableVehicle(vehicles []Vehicle, now time.Time) (Vehicle, bool) {
	for _, vehicle := range vehicles {
		if vehicle.Status != VehicleApproved {
			continue
		}
		if vehicle.APKExpiresAt.IsZero() || !now.Before(vehicle.APKExpiresAt) {
			continue
		}
		return vehicle, true
	}
	return Vehicle{}, false
}
