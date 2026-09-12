package repository_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/fleet/internal/domain"
	"github.com/ishakdeveloper/surge/services/fleet/internal/infrastructure/repository"
	"github.com/ishakdeveloper/surge/services/fleet/internal/service"
	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/ishakdeveloper/surge/shared/pgtest"
	"github.com/ishakdeveloper/surge/shared/wire"
	"github.com/jackc/pgx/v5/pgxpool"
)

var t0 = time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)

func newRepository(t *testing.T) (*repository.Postgres, *pgxpool.Pool) {
	t.Helper()
	pool := pgtest.Pool(t)
	return repository.NewPostgres(pool), pool
}

func vehicle(id, driverID, plate string, status domain.VehicleStatus) domain.Vehicle {
	return domain.Vehicle{
		ID: id, DriverID: driverID, Plate: plate, Make: "Toyota", Model: "Toyota Prius",
		Colour: "Wit", Seats: 5, PackageSlug: "sedan", Status: status,
		APKExpiresAt: t0.AddDate(1, 0, 0), TaxiRegistered: true, Insured: true,
		RegisterCheckedAt: t0, CreatedAt: t0, UpdatedAt: t0,
	}
}

func document(id, driverID string, kind domain.DocumentKind, status domain.DocumentStatus, expires time.Time) domain.Document {
	return domain.Document{
		ID: id, DriverID: driverID, Kind: kind, Status: status, ExpiresAt: expires,
		ObjectKey: "documents/" + driverID + "/" + id + ".pdf", ContentType: "application/pdf",
		ByteSize: 2 << 20, CreatedAt: t0, UpdatedAt: t0,
	}
}

func TestADriverAndTheNewsOfThemAreStoredTogether(t *testing.T) {
	repo, pool := newRepository(t)
	ctx := context.Background()

	driver, err := repo.EnsureDriver(ctx, "drv-1", t0)
	if err != nil {
		t.Fatal(err)
	}
	if driver.Status != domain.StatusOnboarding || driver.Identity != "" && driver.Identity != domain.IdentityUnstarted {
		t.Fatalf("a new driver is %+v", driver)
	}
	// Twice is the same driver, because two requests can arrive together.
	if again, err := repo.EnsureDriver(ctx, "drv-1", t0.Add(time.Minute)); err != nil || !again.CreatedAt.Equal(driver.CreatedAt) {
		t.Errorf("a second ensure made a second driver: %v", err)
	}

	driver.Identity = domain.IdentityVerified
	driver.VerifiedName = "Jan de Vries"
	driver.LicenceExpiresAt = t0.AddDate(5, 0, 0)
	driver.Status = domain.StatusApproved
	driver.ApprovedAt = t0
	driver.UpdatedAt = t0
	err = repo.SaveDriver(ctx, driver, &service.Fact{
		DriverID: "drv-1", Status: domain.StatusApproved, Plate: "02JLT3", PackageSlug: "sedan", At: t0,
	})
	if err != nil {
		t.Fatal(err)
	}

	stored, err := repo.Driver(ctx, "drv-1")
	if err != nil || stored.VerifiedName != "Jan de Vries" || !stored.LicenceExpiresAt.Equal(driver.LicenceExpiresAt) {
		t.Fatalf("stored %+v (%v)", stored, err)
	}
	if found, err := repo.DriverBySession(ctx, "nothing"); err == nil {
		t.Errorf("a session nobody started found %s", found.ID)
	}

	// The fact went into the outbox in the same transaction.
	var (
		topic, key string
		value      []byte
	)
	err = pool.QueryRow(ctx, `select topic, key, value from fleet_outbox order by id desc limit 1`).
		Scan(&topic, &key, &value)
	if err != nil {
		t.Fatalf("no news was written: %v", err)
	}
	if topic != kafkax.TopicFleetDrivers || key != "drv-1" {
		t.Errorf("published to %s keyed %q", topic, key)
	}
	var fact wire.FleetFact
	if err := json.Unmarshal(value, &fact); err != nil || !fact.Valid() {
		t.Fatalf("the news is not usable: %s (%v)", value, err)
	}
	if fact.Tag != wire.FactDriverApproved || fact.Plate != "02JLT3" {
		t.Errorf("the news says %+v", fact)
	}
}

// One live registration per plate, whoever asks second — and retiring one
// frees it, which is what happens when a driver sells the car.
func TestAPlateIsHeldByOneCarAtATime(t *testing.T) {
	repo, _ := newRepository(t)
	ctx := context.Background()

	for _, driverID := range []string{"drv-1", "drv-2"} {
		if _, err := repo.EnsureDriver(ctx, driverID, t0); err != nil {
			t.Fatal(err)
		}
	}

	if err := repo.SaveVehicle(ctx, vehicle("veh-1", "drv-1", "02JLT3", domain.VehiclePending)); err != nil {
		t.Fatal(err)
	}
	err := repo.SaveVehicle(ctx, vehicle("veh-2", "drv-2", "02JLT3", domain.VehiclePending))
	if !errors.Is(err, domain.ErrPlateTaken) {
		t.Fatalf("a second driver took the plate: %v", err)
	}

	retired := vehicle("veh-1", "drv-1", "02JLT3", domain.VehicleRetired)
	if err := repo.SaveVehicle(ctx, retired); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveVehicle(ctx, vehicle("veh-2", "drv-2", "02JLT3", domain.VehiclePending)); err != nil {
		t.Errorf("the plate was still held after the car was retired: %v", err)
	}
}

func TestDocumentsKeepWhatWasReadOffThem(t *testing.T) {
	repo, _ := newRepository(t)
	ctx := context.Background()
	if _, err := repo.EnsureDriver(ctx, "drv-1", t0); err != nil {
		t.Fatal(err)
	}

	insurance := document("doc-1", "drv-1", domain.KindInsurance, domain.Submitted, time.Time{})
	insurance.Extracted = domain.Extraction{
		Status: domain.ExtractionDone, Insurer: "Achmea", PolicyNumber: "NL-8842-11",
		ExpiresAt: t0.AddDate(1, 0, 0), Quotes: []string{"geldig tot 31-03-2027"},
	}
	if err := repo.SaveDocument(ctx, insurance); err != nil {
		t.Fatal(err)
	}

	stored, err := repo.Document(ctx, "doc-1")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Extracted.Insurer != "Achmea" || stored.Extracted.Status != domain.ExtractionDone {
		t.Errorf("stored %+v", stored.Extracted)
	}
	if len(stored.Extracted.Quotes) != 1 {
		t.Errorf("the quotes did not survive: %v", stored.Extracted.Quotes)
	}
	// What was read off a certificate is a date, not a moment: the time of day
	// is dropped on the way in rather than invented.
	if want := t0.AddDate(1, 0, 0).Truncate(24 * time.Hour); !stored.Extracted.ExpiresAt.Equal(want) {
		t.Errorf("the read expiry came back as %v, want %v", stored.Extracted.ExpiresAt, want)
	}
}

// The queue is a queue: whoever has been waiting longest is first, and a page
// picks up where the last one left off.
func TestTheQueueIsOrderedByWhoHasWaitedLongest(t *testing.T) {
	repo, _ := newRepository(t)
	ctx := context.Background()

	for i, driverID := range []string{"drv-1", "drv-2", "drv-3"} {
		if _, err := repo.EnsureDriver(ctx, driverID, t0); err != nil {
			t.Fatal(err)
		}
		waiting := document("doc-"+driverID, driverID, domain.KindInsurance, domain.Submitted, time.Time{})
		// drv-1 has been waiting longest.
		waiting.CreatedAt = t0.Add(time.Duration(i) * time.Hour)
		if err := repo.SaveDocument(ctx, waiting); err != nil {
			t.Fatal(err)
		}
	}
	// A document nobody is waiting on does not put its driver in the queue.
	decided := document("doc-decided", "drv-1", domain.KindVOG, domain.Approved, t0.AddDate(1, 0, 0))
	if err := repo.SaveDocument(ctx, decided); err != nil {
		t.Fatal(err)
	}

	page, err := repo.Queue(ctx, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].Driver.ID != "drv-1" || page.Items[1].Driver.ID != "drv-2" {
		t.Fatalf("first page %+v", page.Items)
	}
	if page.Items[0].Waiting != 1 || !page.Items[0].Since.Equal(t0) {
		t.Errorf("drv-1 is waiting on %d since %v", page.Items[0].Waiting, page.Items[0].Since)
	}
	if page.NextCursor != "drv-2" {
		t.Fatalf("cursor %q", page.NextCursor)
	}

	next, err := repo.Queue(ctx, 2, page.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Items) != 1 || next.Items[0].Driver.ID != "drv-3" || next.NextCursor != "" {
		t.Errorf("second page %+v cursor %q", next.Items, next.NextCursor)
	}
}

func TestExpiringWhatHasRunOut(t *testing.T) {
	repo, _ := newRepository(t)
	ctx := context.Background()
	if _, err := repo.EnsureDriver(ctx, "drv-1", t0); err != nil {
		t.Fatal(err)
	}

	lapsed := document("doc-1", "drv-1", domain.KindInsurance, domain.Approved, t0.AddDate(0, 1, 0))
	current := document("doc-2", "drv-1", domain.KindVOG, domain.Approved, t0.AddDate(1, 0, 0))
	// A registration has no date at all, and never expires.
	undated := document("doc-3", "drv-1", domain.KindRegistration, domain.Approved, time.Time{})
	for _, stored := range []domain.Document{lapsed, current, undated} {
		if err := repo.SaveDocument(ctx, stored); err != nil {
			t.Fatal(err)
		}
	}

	drivers, err := repo.ExpireDocuments(ctx, t0.AddDate(0, 2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(drivers) != 1 || drivers[0] != "drv-1" {
		t.Fatalf("expired for %v", drivers)
	}

	papers, err := repo.Papers(ctx, "drv-1")
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]domain.DocumentStatus{}
	for _, stored := range papers.Documents {
		byID[stored.ID] = stored.Status
	}
	if byID["doc-1"] != domain.Expired {
		t.Errorf("the lapsed certificate is %s", byID["doc-1"])
	}
	if byID["doc-2"] != domain.Approved || byID["doc-3"] != domain.Approved {
		t.Errorf("something still current was expired: %v", byID)
	}

	// Twice changes nothing.
	if again, err := repo.ExpireDocuments(ctx, t0.AddDate(0, 2, 0)); err != nil || len(again) != 0 {
		t.Errorf("a second sweep expired %v (%v)", again, err)
	}
}
