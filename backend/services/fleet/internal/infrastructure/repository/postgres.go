// Package repository is where the fleet's drivers, vehicles and documents
// live.
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ishakdeveloper/surge/services/fleet/internal/domain"
	"github.com/ishakdeveloper/surge/services/fleet/internal/service"
	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/ishakdeveloper/surge/shared/outbox"
	"github.com/ishakdeveloper/surge/shared/wire"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OutboxTable is where fleet facts wait for the relay.
var OutboxTable = outbox.NewTable("fleet_outbox")

type Postgres struct{ pool *pgxpool.Pool }

func NewPostgres(pool *pgxpool.Pool) *Postgres { return &Postgres{pool: pool} }

type scanner interface{ Scan(dest ...any) error }

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}

func nullable(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func at(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}

// --- drivers -----------------------------------------------------------------

const driverColumns = `driver_id, status, blocked_reason, identity_status, identity_session_id,
	verified_name, verified_document, licence_expires_at, approved_at, created_at, updated_at`

func scanDriver(row scanner) (domain.Driver, error) {
	var (
		driver            domain.Driver
		status, identity  string
		licence, approved *time.Time
	)
	err := row.Scan(&driver.ID, &status, &driver.BlockedReason, &identity, &driver.IdentitySessionID,
		&driver.VerifiedName, &driver.VerifiedDocument, &licence, &approved,
		&driver.CreatedAt, &driver.UpdatedAt)
	if err != nil {
		return domain.Driver{}, err
	}
	driver.Status = domain.Status(status)
	driver.Identity = domain.IdentityStatus(identity)
	driver.LicenceExpiresAt = at(licence)
	driver.ApprovedAt = at(approved)
	return driver, nil
}

func (r *Postgres) EnsureDriver(ctx context.Context, driverID string, now time.Time) (domain.Driver, error) {
	_, err := r.pool.Exec(ctx, `
		insert into fleet_driver (driver_id, created_at, updated_at)
		values ($1, $2, $2)
		on conflict (driver_id) do nothing`, driverID, now)
	if err != nil {
		return domain.Driver{}, fmt.Errorf("repository: ensure driver: %w", err)
	}
	return r.Driver(ctx, driverID)
}

func (r *Postgres) Driver(ctx context.Context, driverID string) (domain.Driver, error) {
	driver, err := scanDriver(r.pool.QueryRow(ctx,
		`select `+driverColumns+` from fleet_driver where driver_id = $1`, driverID))
	if err != nil {
		return domain.Driver{}, fmt.Errorf("repository: driver: %w", notFound(err))
	}
	return driver, nil
}

func (r *Postgres) DriverBySession(ctx context.Context, sessionID string) (domain.Driver, error) {
	driver, err := scanDriver(r.pool.QueryRow(ctx,
		`select `+driverColumns+` from fleet_driver
		 where identity_session_id = $1 and identity_session_id <> ''`, sessionID))
	if err != nil {
		return domain.Driver{}, fmt.Errorf("repository: driver by session: %w", notFound(err))
	}
	return driver, nil
}

// SaveDriver stores the driver and, when there is news, the news of it, in one
// transaction — so a standing that changed is never a fact nobody hears.
func (r *Postgres) SaveDriver(ctx context.Context, driver domain.Driver, fact *service.Fact) error {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			update fleet_driver set
			  status = $2, blocked_reason = $3, identity_status = $4, identity_session_id = $5,
			  verified_name = $6, verified_document = $7, licence_expires_at = $8,
			  approved_at = $9, updated_at = $10
			where driver_id = $1`,
			driver.ID, string(driver.Status), driver.BlockedReason, string(driver.Identity),
			driver.IdentitySessionID, driver.VerifiedName, driver.VerifiedDocument,
			nullable(driver.LicenceExpiresAt), nullable(driver.ApprovedAt), driver.UpdatedAt)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		if fact == nil {
			return nil
		}
		return outbox.Write(ctx, tx, OutboxTable, outbox.Message{
			Topic: kafkax.TopicFleetDrivers,
			Key:   fact.DriverID,
			Value: factOf(*fact, driver),
		})
	})
	if err != nil {
		return fmt.Errorf("repository: save driver: %w", err)
	}
	return nil
}

// factOf is the news of a standing, in the shape the rest of the system reads.
func factOf(fact service.Fact, driver domain.Driver) wire.FleetFact {
	if fact.Status == domain.StatusApproved {
		return wire.FleetFact{
			Tag: wire.FactDriverApproved, DriverID: fact.DriverID,
			Plate: fact.Plate, PackageSlug: fact.PackageSlug, AtMs: fact.At.UnixMilli(),
		}
	}
	reason := driver.BlockedReason
	if reason == "" {
		reason = "their paperwork is not complete"
	}
	return wire.FleetFact{
		Tag: wire.FactDriverWithdrawn, DriverID: fact.DriverID,
		Reason: reason, AtMs: fact.At.UnixMilli(),
	}
}

func (r *Postgres) Papers(ctx context.Context, driverID string) (domain.Papers, error) {
	driver, err := r.Driver(ctx, driverID)
	if err != nil {
		return domain.Papers{}, err
	}
	vehicles, err := r.vehicles(ctx, driverID)
	if err != nil {
		return domain.Papers{}, err
	}
	documents, err := r.documents(ctx, driverID)
	if err != nil {
		return domain.Papers{}, err
	}
	return domain.Papers{Driver: driver, Vehicles: vehicles, Documents: documents}, nil
}

// --- vehicles ----------------------------------------------------------------

const vehicleColumns = `id, driver_id, plate, make, model, colour, seats, package_slug,
	status, rejected_reason, apk_expires_at, taxi_registered, insured,
	first_registered_at, register_checked_at, created_at, updated_at`

func scanVehicle(row scanner) (domain.Vehicle, error) {
	var (
		vehicle             domain.Vehicle
		status              string
		apk, first, checked *time.Time
	)
	err := row.Scan(&vehicle.ID, &vehicle.DriverID, &vehicle.Plate, &vehicle.Make, &vehicle.Model,
		&vehicle.Colour, &vehicle.Seats, &vehicle.PackageSlug, &status, &vehicle.RejectedReason,
		&apk, &vehicle.TaxiRegistered, &vehicle.Insured, &first, &checked,
		&vehicle.CreatedAt, &vehicle.UpdatedAt)
	if err != nil {
		return domain.Vehicle{}, err
	}
	vehicle.Status = domain.VehicleStatus(status)
	vehicle.APKExpiresAt = at(apk)
	vehicle.FirstRegisteredAt = at(first)
	vehicle.RegisterCheckedAt = at(checked)
	return vehicle, nil
}

func (r *Postgres) Vehicle(ctx context.Context, vehicleID string) (domain.Vehicle, error) {
	vehicle, err := scanVehicle(r.pool.QueryRow(ctx,
		`select `+vehicleColumns+` from fleet_vehicle where id = $1`, vehicleID))
	if err != nil {
		return domain.Vehicle{}, fmt.Errorf("repository: vehicle: %w", notFound(err))
	}
	return vehicle, nil
}

func (r *Postgres) vehicles(ctx context.Context, driverID string) ([]domain.Vehicle, error) {
	rows, err := r.pool.Query(ctx,
		`select `+vehicleColumns+` from fleet_vehicle where driver_id = $1 order by created_at desc`, driverID)
	if err != nil {
		return nil, fmt.Errorf("repository: vehicles: %w", err)
	}
	vehicles, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Vehicle, error) {
		return scanVehicle(row)
	})
	if err != nil {
		return nil, fmt.Errorf("repository: vehicles: %w", err)
	}
	return vehicles, nil
}

// SaveVehicle stores a car, refusing a plate somebody else still offers.
func (r *Postgres) SaveVehicle(ctx context.Context, vehicle domain.Vehicle) error {
	_, err := r.pool.Exec(ctx, `
		insert into fleet_vehicle (id, driver_id, plate, make, model, colour, seats, package_slug,
		  status, rejected_reason, apk_expires_at, taxi_registered, insured,
		  first_registered_at, register_checked_at, created_at, updated_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		on conflict (id) do update set
		  status = excluded.status, rejected_reason = excluded.rejected_reason,
		  apk_expires_at = excluded.apk_expires_at, taxi_registered = excluded.taxi_registered,
		  insured = excluded.insured, register_checked_at = excluded.register_checked_at,
		  updated_at = excluded.updated_at`,
		vehicle.ID, vehicle.DriverID, vehicle.Plate, vehicle.Make, vehicle.Model, vehicle.Colour,
		vehicle.Seats, vehicle.PackageSlug, string(vehicle.Status), vehicle.RejectedReason,
		nullable(vehicle.APKExpiresAt), vehicle.TaxiRegistered, vehicle.Insured,
		nullable(vehicle.FirstRegisteredAt), nullable(vehicle.RegisterCheckedAt),
		vehicle.CreatedAt, vehicle.UpdatedAt)

	var violation *pgconn.PgError
	if errors.As(err, &violation) && violation.Code == uniqueViolation &&
		strings.Contains(violation.ConstraintName, "plate") {
		return domain.ErrPlateTaken
	}
	if err != nil {
		return fmt.Errorf("repository: save vehicle: %w", err)
	}
	return nil
}

// uniqueViolation is Postgres' code for a unique index refusing a row.
const uniqueViolation = "23505"

// --- documents ---------------------------------------------------------------

const documentColumns = `id, driver_id, vehicle_id, kind, status, object_key, content_type,
	byte_size, expires_at, extracted, extraction, reviewer_id, review_note, reviewed_at,
	created_at, updated_at`

// storedExtraction is what a reading looks like in the column.
type storedExtraction struct {
	Insurer      string   `json:"insurer,omitempty"`
	PolicyNumber string   `json:"policyNumber,omitempty"`
	ExpiresAt    string   `json:"expiresAt,omitempty"`
	Quotes       []string `json:"quotes,omitempty"`
}

func scanDocument(row scanner) (domain.Document, error) {
	var (
		document          domain.Document
		kind, status      string
		extraction        string
		extracted         []byte
		expires, reviewed *time.Time
	)
	err := row.Scan(&document.ID, &document.DriverID, &document.VehicleID, &kind, &status,
		&document.ObjectKey, &document.ContentType, &document.ByteSize, &expires, &extracted,
		&extraction, &document.ReviewerID, &document.ReviewNote, &reviewed,
		&document.CreatedAt, &document.UpdatedAt)
	if err != nil {
		return domain.Document{}, err
	}
	document.Kind = domain.DocumentKind(kind)
	document.Status = domain.DocumentStatus(status)
	document.ExpiresAt = at(expires)
	document.ReviewedAt = at(reviewed)

	document.Extracted = domain.Extraction{Status: domain.ExtractionStatus(extraction)}
	var stored storedExtraction
	if len(extracted) > 0 && json.Unmarshal(extracted, &stored) == nil {
		document.Extracted.Insurer = stored.Insurer
		document.Extracted.PolicyNumber = stored.PolicyNumber
		document.Extracted.Quotes = stored.Quotes
		if parsed, err := time.ParseInLocation(time.DateOnly, stored.ExpiresAt, time.UTC); err == nil {
			document.Extracted.ExpiresAt = parsed
		}
	}
	return document, nil
}

func (r *Postgres) Document(ctx context.Context, documentID string) (domain.Document, error) {
	document, err := scanDocument(r.pool.QueryRow(ctx,
		`select `+documentColumns+` from fleet_document where id = $1`, documentID))
	if err != nil {
		return domain.Document{}, fmt.Errorf("repository: document: %w", notFound(err))
	}
	return document, nil
}

func (r *Postgres) documents(ctx context.Context, driverID string) ([]domain.Document, error) {
	rows, err := r.pool.Query(ctx,
		`select `+documentColumns+` from fleet_document where driver_id = $1 order by created_at`, driverID)
	if err != nil {
		return nil, fmt.Errorf("repository: documents: %w", err)
	}
	documents, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Document, error) {
		return scanDocument(row)
	})
	if err != nil {
		return nil, fmt.Errorf("repository: documents: %w", err)
	}
	return documents, nil
}

func (r *Postgres) SaveDocument(ctx context.Context, document domain.Document) error {
	extracted, err := json.Marshal(storedExtraction{
		Insurer:      document.Extracted.Insurer,
		PolicyNumber: document.Extracted.PolicyNumber,
		ExpiresAt:    dateOnly(document.Extracted.ExpiresAt),
		Quotes:       document.Extracted.Quotes,
	})
	if err != nil {
		return fmt.Errorf("repository: encode extraction: %w", err)
	}

	_, err = r.pool.Exec(ctx, `
		insert into fleet_document (id, driver_id, vehicle_id, kind, status, object_key,
		  content_type, byte_size, expires_at, extracted, extraction, reviewer_id, review_note,
		  reviewed_at, created_at, updated_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		on conflict (id) do update set
		  status = excluded.status, object_key = excluded.object_key,
		  content_type = excluded.content_type, byte_size = excluded.byte_size,
		  expires_at = excluded.expires_at, extracted = excluded.extracted,
		  extraction = excluded.extraction, reviewer_id = excluded.reviewer_id,
		  review_note = excluded.review_note, reviewed_at = excluded.reviewed_at,
		  updated_at = excluded.updated_at`,
		document.ID, document.DriverID, document.VehicleID, string(document.Kind),
		string(document.Status), document.ObjectKey, document.ContentType, document.ByteSize,
		nullable(document.ExpiresAt), extracted, string(document.Extracted.Status),
		document.ReviewerID, document.ReviewNote, nullable(document.ReviewedAt),
		document.CreatedAt, document.UpdatedAt)
	if err != nil {
		return fmt.Errorf("repository: save document: %w", err)
	}
	return nil
}

func dateOnly(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.DateOnly)
}

// --- the queue and the sweeper ------------------------------------------------

// QueueLimit clamps a page of the review queue.
func QueueLimit(requested int) int {
	switch {
	case requested <= 0:
		return 25
	case requested > 100:
		return 100
	default:
		return requested
	}
}

// Queue is who is waiting for a reviewer, longest first.
//
// Ordered by the oldest document still waiting rather than by when the driver
// signed up: the queue is a queue, and somebody who sent one more certificate
// this morning has not gone to the back of it.
func (r *Postgres) Queue(ctx context.Context, limit int, cursor string) (service.QueuePage, error) {
	limit = QueueLimit(limit)

	query := `
		with waiting as (
		  select driver_id, count(*)::int as waiting, min(created_at) as since
		  from fleet_document where status = 'submitted' group by driver_id
		)
		select ` + strings.ReplaceAll("d."+driverColumns, ", ", ", d.") + `, w.waiting, w.since
		from waiting w join fleet_driver d on d.driver_id = w.driver_id`
	args := []any{}
	if cursor != "" {
		query += `
		where (w.since, d.driver_id) > (
		  select w2.since, $1::text from waiting w2 where w2.driver_id = $1
		)`
		args = append(args, cursor)
	}
	query += fmt.Sprintf(" order by w.since, d.driver_id limit $%d", len(args)+1)
	args = append(args, limit+1)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return service.QueuePage{}, fmt.Errorf("repository: queue: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (service.QueueItem, error) {
		var (
			item    service.QueueItem
			waiting int
			since   time.Time
		)
		driver, err := scanQueueRow(row, &waiting, &since)
		item.Driver, item.Waiting, item.Since = driver, waiting, since
		return item, err
	})
	if err != nil {
		return service.QueuePage{}, fmt.Errorf("repository: queue: %w", err)
	}

	page := service.QueuePage{}
	if len(items) > limit {
		items = items[:limit]
		page.NextCursor = items[limit-1].Driver.ID
	}
	page.Items = items
	return page, nil
}

// scanQueueRow reads a driver plus the two aggregates beside them.
func scanQueueRow(row pgx.CollectableRow, waiting *int, since *time.Time) (domain.Driver, error) {
	var (
		driver            domain.Driver
		status, identity  string
		licence, approved *time.Time
	)
	err := row.Scan(&driver.ID, &status, &driver.BlockedReason, &identity, &driver.IdentitySessionID,
		&driver.VerifiedName, &driver.VerifiedDocument, &licence, &approved,
		&driver.CreatedAt, &driver.UpdatedAt, waiting, since)
	if err != nil {
		return domain.Driver{}, err
	}
	driver.Status = domain.Status(status)
	driver.Identity = domain.IdentityStatus(identity)
	driver.LicenceExpiresAt = at(licence)
	driver.ApprovedAt = at(approved)
	return driver, nil
}

// ExpireDocuments moves what has run out to expired, and says whose it was.
func (r *Postgres) ExpireDocuments(ctx context.Context, now time.Time) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		with expired as (
		  update fleet_document set status = 'expired', updated_at = $1
		  where status = 'approved' and expires_at is not null and expires_at <= $1
		  returning driver_id
		)
		select distinct driver_id from expired`, now)
	if err != nil {
		return nil, fmt.Errorf("repository: expire documents: %w", err)
	}
	drivers, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (string, error) {
		var driverID string
		return driverID, row.Scan(&driverID)
	})
	if err != nil {
		return nil, fmt.Errorf("repository: expire documents: %w", err)
	}
	return drivers, nil
}
