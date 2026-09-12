package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/fleet/internal/domain"
)

var now = time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)

func year(years int) time.Time { return now.AddDate(years, 0, 0) }

func sedan() domain.Class {
	class, _ := domain.ClassBySlug("sedan")
	return class
}

// A plate is typed by a person on a phone, and the register holds one spelling.
func TestNormalizePlate(t *testing.T) {
	for _, typed := range []string{"02-JLT-3", "02 jlt 3", "02JLT3", " 02jlt3 "} {
		plate, err := domain.NormalizePlate(typed)
		if err != nil || plate != "02JLT3" {
			t.Errorf("%q became %q (%v)", typed, plate, err)
		}
	}
	for _, wrong := range []string{"02JLT", "02JLT33", "", "02/JLT/3"} {
		if _, err := domain.NormalizePlate(wrong); err == nil {
			t.Errorf("%q was accepted", wrong)
		}
	}
}

// What the register says decides whether a car can be offered at all. The
// refusals are facts; the notes are for a person to weigh.
func TestCheckRegistration(t *testing.T) {
	good := domain.Registration{
		Plate: "02JLT3", Seats: 5, APKExpiresAt: year(1), Insured: true, TaxiRegistered: true,
	}

	notes, err := domain.CheckRegistration(good, sedan(), now)
	if err != nil || len(notes) != 0 {
		t.Fatalf("a taxi-registered, insured, inspected car: %v %v", notes, err)
	}

	lapsed := good
	lapsed.APKExpiresAt = now.AddDate(0, -1, 0)
	if _, err := domain.CheckRegistration(lapsed, sedan(), now); err == nil {
		t.Error("a car whose inspection lapsed was accepted")
	}

	uninsured := good
	uninsured.Insured = false
	if _, err := domain.CheckRegistration(uninsured, sedan(), now); err == nil {
		t.Error("an uninsured car was accepted")
	}

	unknown := good
	unknown.APKExpiresAt = time.Time{}
	if _, err := domain.CheckRegistration(unknown, sedan(), now); err == nil {
		t.Error("a car the register has no inspection date for was accepted")
	}

	// Five seats is a Surge, not a Surge XL.
	van, _ := domain.ClassBySlug("van")
	if _, err := domain.CheckRegistration(good, van, now); err == nil {
		t.Error("a five-seat car was accepted for six-seat work")
	}

	// Not a refusal: a car can be registered for taxi use after it is bought,
	// and that is a conversation rather than a rejection.
	notTaxi := good
	notTaxi.TaxiRegistered = false
	notes, err = domain.CheckRegistration(notTaxi, sedan(), now)
	if err != nil || len(notes) != 1 {
		t.Errorf("a car not down for taxi use: %v %v", notes, err)
	}

	// An inspection that runs out next month is worth saying out loud.
	soon := good
	soon.APKExpiresAt = now.AddDate(0, 0, 10)
	if notes, _ := domain.CheckRegistration(soon, sedan(), now); len(notes) != 1 {
		t.Errorf("an inspection due in ten days went unmentioned: %v", notes)
	}
}

// The whole point of the service, as a table: what a driver still owes.
func TestOutstanding(t *testing.T) {
	verified := domain.Driver{
		ID: "drv-1", Status: domain.StatusOnboarding,
		Identity: domain.IdentityVerified, LicenceExpiresAt: year(4),
	}
	car := domain.Vehicle{
		ID: "veh-1", DriverID: "drv-1", Plate: "02JLT3",
		Status: domain.VehicleApproved, APKExpiresAt: year(1),
	}
	valid := func(kind domain.DocumentKind, vehicleID string) domain.Document {
		return domain.Document{
			DriverID: "drv-1", VehicleID: vehicleID, Kind: kind,
			Status: domain.Approved, ExpiresAt: year(1),
		}
	}
	complete := domain.Papers{
		Driver:   verified,
		Vehicles: []domain.Vehicle{car},
		Documents: []domain.Document{
			valid(domain.KindInsurance, "veh-1"),
			valid(domain.KindVOG, ""),
			valid(domain.KindChauffeurskaart, ""),
		},
	}

	if outstanding := domain.Outstanding(complete, now); len(outstanding) != 0 {
		t.Fatalf("a complete driver still owes %+v", outstanding)
	}
	if standing := domain.StandingOf(complete, now); standing != domain.StatusApproved {
		t.Fatalf("a complete driver is %s", standing)
	}

	cases := []struct {
		name   string
		change func(*domain.Papers)
		want   domain.RequirementKind
	}{
		{"unverified", func(p *domain.Papers) { p.Driver.Identity = domain.IdentityUnstarted }, domain.NeedIdentity},
		{"a licence that lapsed", func(p *domain.Papers) { p.Driver.LicenceExpiresAt = now.AddDate(0, -1, 0) }, domain.NeedIdentity},
		{"no car", func(p *domain.Papers) { p.Vehicles = nil }, domain.NeedVehicle},
		{"a car nobody approved", func(p *domain.Papers) { p.Vehicles[0].Status = domain.VehiclePending }, domain.NeedVehicle},
		// The car is approved and its inspection has since run out: a standing
		// that was worked out once and remembered would have missed this.
		{"an inspection that ran out", func(p *domain.Papers) { p.Vehicles[0].APKExpiresAt = now.AddDate(0, -1, 0) }, domain.NeedVehicle},
		{"insurance that expired", func(p *domain.Papers) { p.Documents[0].ExpiresAt = now.AddDate(0, -1, 0) }, domain.NeedInsurance},
		{"an unapproved VOG", func(p *domain.Papers) { p.Documents[1].Status = domain.Submitted }, domain.NeedVOG},
		{"no chauffeurskaart yet", func(p *domain.Papers) { p.Documents[2].Status = domain.AwaitingAuthority }, domain.NeedChauffeurskaart},
	}
	for _, c := range cases {
		papers := domain.Papers{
			Driver:    complete.Driver,
			Vehicles:  append([]domain.Vehicle(nil), complete.Vehicles...),
			Documents: append([]domain.Document(nil), complete.Documents...),
		}
		c.change(&papers)

		outstanding := domain.Outstanding(papers, now)
		if len(outstanding) == 0 {
			t.Errorf("%s: nothing outstanding", c.name)
			continue
		}
		found := false
		for _, requirement := range outstanding {
			if requirement.Kind == c.want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: outstanding %+v, want %s", c.name, outstanding, c.want)
		}
		if standing := domain.StandingOf(papers, now); standing != domain.StatusOnboarding {
			t.Errorf("%s: standing %s", c.name, standing)
		}
	}

	// A block is a person's decision, and the papers do not overrule it.
	blocked := complete
	blocked.Driver.Status = domain.StatusBlocked
	if standing := domain.StandingOf(blocked, now); standing != domain.StatusBlocked {
		t.Errorf("a blocked driver with perfect papers is %s", standing)
	}
}

func TestCheckUpload(t *testing.T) {
	if err := domain.CheckUpload(domain.KindInsurance, "image/jpeg", 1<<20); err != nil {
		t.Errorf("a photograph of a certificate: %v", err)
	}
	for _, refused := range []struct {
		name        string
		kind        domain.DocumentKind
		contentType string
		size        int64
	}{
		{"a kind nobody asks for", "passport", "image/jpeg", 1 << 20},
		{"a type nobody can read", domain.KindInsurance, "application/zip", 1 << 20},
		{"nothing at all", domain.KindInsurance, "image/jpeg", 0},
		{"more than the limit", domain.KindInsurance, "application/pdf", domain.MaxUploadBytes + 1},
	} {
		if err := domain.CheckUpload(refused.kind, refused.contentType, refused.size); err == nil {
			t.Errorf("%s was accepted", refused.name)
		}
	}
}

// A reviewer has to read a date off anything that expires, and has to say why
// when they refuse.
func TestCheckDecision(t *testing.T) {
	submitted := domain.Document{Kind: domain.KindInsurance, Status: domain.Submitted}

	if err := domain.CheckDecision(submitted, domain.Decision{Approve: true, ExpiresAt: year(1)}, now); err != nil {
		t.Errorf("approving with a date: %v", err)
	}
	if err := domain.CheckDecision(submitted, domain.Decision{Approve: true}, now); err == nil {
		t.Error("approved an insurance certificate with no expiry")
	}
	if err := domain.CheckDecision(submitted, domain.Decision{Approve: true, ExpiresAt: now.AddDate(0, -1, 0)}, now); err == nil {
		t.Error("approved a certificate that had already expired")
	}
	if err := domain.CheckDecision(submitted, domain.Decision{}, now); err == nil {
		t.Error("rejected a document without saying why")
	}
	if err := domain.CheckDecision(submitted, domain.Decision{Note: "it is last year's"}, now); err != nil {
		t.Errorf("rejecting with a reason: %v", err)
	}

	// A registration document does not expire, so no date is asked for.
	registration := domain.Document{Kind: domain.KindRegistration, Status: domain.Submitted}
	if err := domain.CheckDecision(registration, domain.Decision{Approve: true}, now); err != nil {
		t.Errorf("approving a registration: %v", err)
	}

	waiting := domain.Document{Kind: domain.KindVOG, Status: domain.AwaitingFile}
	var invalid *domain.InvalidError
	if err := domain.CheckDecision(waiting, domain.Decision{Approve: true, ExpiresAt: year(1)}, now); !errors.As(err, &invalid) {
		t.Errorf("decided a document nobody has sent: %v", err)
	}
}

func TestDocumentValidity(t *testing.T) {
	approved := domain.Document{Status: domain.Approved, ExpiresAt: year(1)}
	if !approved.ValidAt(now) {
		t.Error("an approved, unexpired document does not count")
	}
	if approved.ValidAt(year(2)) {
		t.Error("an expired document still counts")
	}
	if (domain.Document{Status: domain.Submitted}).ValidAt(now) {
		t.Error("a document nobody has looked at counts")
	}
	// A registration has no date, and counts until somebody says otherwise.
	if !(domain.Document{Status: domain.Approved}).ValidAt(now) {
		t.Error("an approved document with no expiry does not count")
	}
}
