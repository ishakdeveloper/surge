package service_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/fleet/internal/domain"
	"github.com/ishakdeveloper/surge/services/fleet/internal/infrastructure/fake"
	"github.com/ishakdeveloper/surge/services/fleet/internal/infrastructure/repository"
	"github.com/ishakdeveloper/surge/services/fleet/internal/infrastructure/storage"
	"github.com/ishakdeveloper/surge/services/fleet/internal/service"
	"github.com/ishakdeveloper/surge/shared/authz"
	"github.com/ishakdeveloper/surge/shared/wire"
)

var start = time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)

var (
	driver   = authz.Identity{UserID: "drv-1", Role: authz.RoleDriver}
	other    = authz.Identity{UserID: "drv-2", Role: authz.RoleDriver}
	reviewer = authz.Identity{UserID: "ops-1", Role: authz.RoleOps}
	rider    = authz.Identity{UserID: "rider-1", Role: authz.RoleRider}
)

type rig struct {
	fleet    *service.Service
	repo     *repository.Memory
	identity *fake.Identity
	store    *storage.Memory
	now      time.Time
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{
		repo:     repository.NewMemory(),
		identity: fake.NewIdentity(),
		store:    storage.NewMemory(),
		now:      start,
	}
	ids := 0
	fleet, err := service.New(service.Options{
		Repository: r.repo,
		Identity:   r.identity,
		Register:   fake.NewRegister(start),
		Store:      r.store,
		Now:        func() time.Time { return r.now },
		NewID:      func() string { ids++; return fmt.Sprintf("id-%d", ids) },
	})
	if err != nil {
		t.Fatal(err)
	}
	r.fleet = fleet
	return r
}

// verify walks the identity check the way the provider's webhook would.
func (r *rig) verify(t *testing.T, who authz.Identity) {
	t.Helper()
	ctx := context.Background()
	session, _, err := r.fleet.StartIdentity(ctx, who, "https://surge.test/drive")
	if err != nil {
		t.Fatalf("start the identity check: %v", err)
	}
	if err := r.fleet.ApplyVerdict(ctx, r.identity.Verdict(session.ID, r.now)); err != nil {
		t.Fatalf("apply the verdict: %v", err)
	}
}

// approveCar adds a car and has a reviewer approve it.
func (r *rig) approveCar(t *testing.T, who authz.Identity, plate string) domain.Vehicle {
	t.Helper()
	ctx := context.Background()
	vehicle, _, err := r.fleet.AddVehicle(ctx, who, plate, "sedan")
	if err != nil {
		t.Fatalf("add %s: %v", plate, err)
	}
	approved, _, err := r.fleet.DecideVehicle(ctx, reviewer, vehicle.ID, true, "")
	if err != nil {
		t.Fatalf("approve %s: %v", plate, err)
	}
	return approved
}

// handIn uploads a document and has a reviewer approve it, with an expiry.
func (r *rig) handIn(t *testing.T, who authz.Identity, kind domain.DocumentKind, vehicleID string, expires time.Time) domain.Document {
	t.Helper()
	ctx := context.Background()

	document, upload, err := r.fleet.StartUpload(ctx, who, kind, vehicleID, "application/pdf", 2<<20)
	if err != nil {
		t.Fatalf("start upload of %s: %v", kind, err)
	}
	if upload.URL == "" || !r.store.Asked(document.ObjectKey) {
		t.Fatalf("no upload link for %s", kind)
	}
	if _, err := r.fleet.FinishUpload(ctx, who, document.ID); err != nil {
		t.Fatalf("finish upload of %s: %v", kind, err)
	}
	decided, _, err := r.fleet.DecideDocument(ctx, reviewer, document.ID, domain.Decision{
		Approve: true, ExpiresAt: expires,
	})
	if err != nil {
		t.Fatalf("approve %s: %v", kind, err)
	}
	return decided
}

func (r *rig) papers(t *testing.T, who authz.Identity) service.View {
	t.Helper()
	view, err := r.fleet.Papers(context.Background(), who)
	if err != nil {
		t.Fatalf("papers: %v", err)
	}
	return view
}

// The whole path: nobody may drive until the register, the identity provider
// and a reviewer all agree, and the standing that follows is published for
// whoever dispatches.
func TestADriverIsApprovedOnlyWhenEverythingHolds(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()

	opening := r.papers(t, driver)
	if opening.Driver.Status != domain.StatusOnboarding || len(opening.Outstanding) == 0 {
		t.Fatalf("a new driver is %s with %d outstanding", opening.Driver.Status, len(opening.Outstanding))
	}

	r.verify(t, driver)
	vehicle := r.approveCar(t, driver, "02JLT3")
	if vehicle.Make != "Toyota" || vehicle.Seats != 5 || !vehicle.TaxiRegistered {
		t.Errorf("the register filled in %+v", vehicle)
	}

	// Still short of the papers, and the driver is told which.
	waiting := r.papers(t, driver)
	if waiting.Driver.Status != domain.StatusApproved && len(waiting.Outstanding) != 3 {
		t.Errorf("after a car: %s, outstanding %+v", waiting.Driver.Status, waiting.Outstanding)
	}

	r.handIn(t, driver, domain.KindInsurance, vehicle.ID, start.AddDate(1, 0, 0))
	r.handIn(t, driver, domain.KindVOG, "", start.AddDate(1, 0, 0))
	r.handIn(t, driver, domain.KindChauffeurskaart, "", start.AddDate(5, 0, 0))

	approved := r.papers(t, driver)
	if approved.Driver.Status != domain.StatusApproved || len(approved.Outstanding) != 0 {
		t.Fatalf("a complete driver is %s owing %+v", approved.Driver.Status, approved.Outstanding)
	}
	if approved.Driver.VerifiedName == "" || approved.Driver.LicenceExpiresAt.IsZero() {
		t.Errorf("the identity check left nothing behind: %+v", approved.Driver)
	}

	// The news the matcher reads, with the car they would be dispatched in.
	facts := r.repo.Facts()
	if len(facts) == 0 {
		t.Fatal("nobody was told")
	}
	last := facts[len(facts)-1]
	if last.Status != domain.StatusApproved || last.Plate != "02JLT3" || last.PackageSlug != "sedan" {
		t.Errorf("the news says %+v", last)
	}

	// An approval is announced once, not on every read.
	before := len(r.repo.Facts())
	r.papers(t, driver)
	if len(r.repo.Facts()) != before {
		t.Error("reading a driver's papers announced something")
	}
	_ = ctx
}

// The register's facts are refusals a person never has to make.
func TestTheRegisterRefusesWhatItCan(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()

	var invalid *domain.InvalidError
	for _, refused := range []struct {
		name  string
		plate string
	}{
		{"an inspection that lapsed", "73RXV1"},
		{"a car nobody insures", "58GDP2"},
		{"a plate the register does not have", "99ZZZ9"},
	} {
		if _, _, err := r.fleet.AddVehicle(ctx, driver, refused.plate, "sedan"); !errors.As(err, &invalid) {
			t.Errorf("%s: %v", refused.name, err)
		}
	}

	// Five seats is not a Surge XL, whatever the driver chose.
	if _, _, err := r.fleet.AddVehicle(ctx, driver, "02JLT3", "van"); !errors.As(err, &invalid) {
		t.Errorf("a five-seat car offered as a van: %v", err)
	}

	// Insured and inspected but not registered for taxi use: allowed, and the
	// reviewer is told rather than the driver refused.
	vehicle, notes, err := r.fleet.AddVehicle(ctx, driver, "91HZK7", "sedan")
	if err != nil || len(notes) == 0 {
		t.Fatalf("a car not down for taxi use: %v %v", notes, err)
	}
	if vehicle.Status != domain.VehiclePending {
		t.Errorf("it went straight to %s", vehicle.Status)
	}

	// One live registration per plate, whoever asks second.
	if _, _, err := r.fleet.AddVehicle(ctx, other, "91HZK7", "sedan"); !errors.Is(err, domain.ErrPlateTaken) {
		t.Errorf("a second driver took the same plate: %v", err)
	}
	// Retiring it frees the plate.
	if _, err := r.fleet.RetireVehicle(ctx, driver, vehicle.ID); err != nil {
		t.Fatalf("retire: %v", err)
	}
	if _, _, err := r.fleet.AddVehicle(ctx, other, "91HZK7", "sedan"); err != nil {
		t.Errorf("the plate was still held after it was retired: %v", err)
	}
}

// A paper that runs out withdraws the driver without anybody deciding
// anything: the standing is derived, and the sweeper only moves the clock.
func TestAPaperRunningOutWithdrawsTheDriver(t *testing.T) {
	r := newRig(t)

	r.verify(t, driver)
	vehicle := r.approveCar(t, driver, "02JLT3")
	r.handIn(t, driver, domain.KindInsurance, vehicle.ID, start.AddDate(0, 1, 0))
	r.handIn(t, driver, domain.KindVOG, "", start.AddDate(1, 0, 0))
	r.handIn(t, driver, domain.KindChauffeurskaart, "", start.AddDate(5, 0, 0))

	if standing := r.papers(t, driver).Driver.Status; standing != domain.StatusApproved {
		t.Fatalf("not approved to begin with: %s", standing)
	}

	// Two months on, the insurance has lapsed.
	r.now = start.AddDate(0, 2, 0)
	swept, err := r.fleet.Sweep(context.Background())
	if err != nil || swept != 1 {
		t.Fatalf("sweep: %d %v", swept, err)
	}

	after := r.papers(t, driver)
	if after.Driver.Status != domain.StatusOnboarding {
		t.Errorf("a driver with lapsed insurance is %s", after.Driver.Status)
	}
	if len(after.Outstanding) != 1 || after.Outstanding[0].Kind != domain.NeedInsurance {
		t.Errorf("outstanding %+v", after.Outstanding)
	}

	last := r.repo.Facts()[len(r.repo.Facts())-1]
	if last.Status == domain.StatusApproved {
		t.Error("the matcher was never told they stopped")
	}

	// Sweeping again changes nothing: the document is already expired.
	if swept, err := r.fleet.Sweep(context.Background()); err != nil || swept != 0 {
		t.Errorf("a second sweep: %d %v", swept, err)
	}
}

// Documents that no API anywhere can answer for wait in the open, with the
// driver able to see what they are waiting for.
func TestPapersFromAnAuthorityWaitVisibly(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()

	document, err := r.fleet.DeclareAuthority(ctx, driver, domain.KindVOG)
	if err != nil {
		t.Fatal(err)
	}
	if document.Status != domain.AwaitingAuthority {
		t.Errorf("a declared VOG is %s", document.Status)
	}
	// Saying so twice is the same application.
	again, err := r.fleet.DeclareAuthority(ctx, driver, domain.KindVOG)
	if err != nil || again.ID != document.ID {
		t.Errorf("a second declaration made %s (%v)", again.ID, err)
	}

	var invalid *domain.InvalidError
	if _, err := r.fleet.DeclareAuthority(ctx, driver, domain.KindInsurance); !errors.As(err, &invalid) {
		t.Errorf("insurance is not issued by an authority: %v", err)
	}

	// When it arrives, the same document takes the file.
	uploaded, _, err := r.fleet.StartUpload(ctx, driver, domain.KindVOG, "", "application/pdf", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if uploaded.ID != document.ID {
		t.Errorf("the file started a second VOG")
	}
}

// Who may ask for what.
func TestRoles(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()

	if _, err := r.fleet.Papers(ctx, rider); !errors.Is(err, domain.ErrNotAllowed) {
		t.Errorf("a rider read a driver's papers: %v", err)
	}
	if _, err := r.fleet.Queue(ctx, driver, 10, ""); !errors.Is(err, domain.ErrNotAllowed) {
		t.Errorf("a driver read the review queue: %v", err)
	}
	if _, err := r.fleet.Papers(ctx, reviewer); !errors.Is(err, domain.ErrNotAllowed) {
		t.Errorf("a reviewer used the driver's own endpoint: %v", err)
	}

	// A driver cannot touch another driver's car.
	vehicle, _, err := r.fleet.AddVehicle(ctx, driver, "02JLT3", "sedan")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.fleet.RetireVehicle(ctx, other, vehicle.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("somebody else retired the car: %v", err)
	}
}

// A reviewer's queue is a queue, and their decisions are what move a driver.
func TestTheReviewQueueAndItsDecisions(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()

	r.verify(t, driver)
	vehicle := r.approveCar(t, driver, "02JLT3")

	document, _, err := r.fleet.StartUpload(ctx, driver, domain.KindInsurance, vehicle.ID, "image/jpeg", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.fleet.FinishUpload(ctx, driver, document.ID); err != nil {
		t.Fatal(err)
	}

	queue, err := r.fleet.Queue(ctx, reviewer, 10, "")
	if err != nil || len(queue.Items) != 1 || queue.Items[0].Waiting != 1 {
		t.Fatalf("queue: %+v %v", queue, err)
	}
	if queue.Items[0].Driver.ID != driver.UserID {
		t.Errorf("the queue holds %s", queue.Items[0].Driver.ID)
	}

	review, err := r.fleet.ReviewItem(ctx, reviewer, driver.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if len(review.Documents) != 1 || review.Documents[0].FileURL == "" {
		t.Errorf("a reviewer cannot open the file: %+v", review.Documents)
	}

	// Rejecting says why, and the driver still owes it.
	rejected, _, err := r.fleet.DecideDocument(ctx, reviewer, document.ID, domain.Decision{
		Note: "this is last year's certificate",
	})
	if err != nil || rejected.Status != domain.Rejected {
		t.Fatalf("reject: %+v %v", rejected, err)
	}
	if rejected.ReviewNote == "" || rejected.ReviewerID != reviewer.UserID {
		t.Errorf("the rejection does not say who or why: %+v", rejected)
	}
	if empty, _ := r.fleet.Queue(ctx, reviewer, 10, ""); len(empty.Items) != 0 {
		t.Errorf("a decided document is still queued")
	}

	// A block is a person's decision, and lifting it returns the driver to
	// whatever their papers say.
	blocked, err := r.fleet.Block(ctx, reviewer, driver.UserID, "reported by three riders")
	if err != nil || blocked.Status != domain.StatusBlocked {
		t.Fatalf("block: %+v %v", blocked, err)
	}
	lifted, err := r.fleet.Block(ctx, reviewer, driver.UserID, "")
	if err != nil || lifted.Status != domain.StatusOnboarding {
		t.Fatalf("unblock: %+v %v", lifted, err)
	}
}

// The fact the matcher reads must carry a car, or there is nothing to
// dispatch.
func TestTheNewsIsUsable(t *testing.T) {
	approved := wire.FleetFact{
		Tag: wire.FactDriverApproved, DriverID: "drv-1", Plate: "02JLT3", PackageSlug: "sedan", AtMs: 1,
	}
	if !approved.Valid() {
		t.Error("an approved driver with a car was refused")
	}
	if (wire.FleetFact{Tag: wire.FactDriverApproved, DriverID: "drv-1"}).Valid() {
		t.Error("an approved driver with no car was accepted")
	}
	if !(wire.FleetFact{Tag: wire.FactDriverWithdrawn, DriverID: "drv-1"}).Valid() {
		t.Error("a withdrawal was refused")
	}
	if (wire.FleetFact{Tag: wire.FactDriverWithdrawn}).Valid() {
		t.Error("a withdrawal about nobody was accepted")
	}
}
