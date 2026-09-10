package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/ishakdeveloper/surge/services/trip/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres is the durable repository.
type Postgres struct{ pool *pgxpool.Pool }

func NewPostgres(pool *pgxpool.Pool) *Postgres { return &Postgres{pool: pool} }

const columns = `id, rider_id, driver_id, status, pickup_lat, pickup_lng, dropoff_lat, dropoff_lng,
	polyline6, meters, seconds, total_cents, surge_multiplier, package_slug,
	idempotency_key, created_at, updated_at`

func (r *Postgres) Create(ctx context.Context, trip *domain.Trip) error {
	_, err := r.pool.Exec(ctx, `
		insert into trip (`+columns+`)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		-- The unique index on (rider_id, idempotency_key) is what actually
		-- enforces "book once". Doing nothing on conflict rather than erroring
		-- means a retry that races the original insert resolves to a read
		-- instead of a failure the caller has to interpret.
		--
		-- The WHERE clause is not optional and not decoration: the index is
		-- partial, because an empty key is not a key and trips without one must
		-- not collide with each other. Postgres will only match ON CONFLICT to
		-- a partial index if the predicate is repeated here, and without it the
		-- insert fails with "no unique or exclusion constraint matching the ON
		-- CONFLICT specification" — at runtime, on the booking path.
		on conflict (rider_id, idempotency_key) where idempotency_key <> '' do nothing`,
		trip.ID, trip.RiderID, nullable(trip.DriverID), string(trip.Status),
		trip.Pickup.Lat, trip.Pickup.Lng, trip.Dropoff.Lat, trip.Dropoff.Lng,
		trip.Polyline6, trip.Meters, trip.Seconds,
		trip.TotalCents, trip.SurgeMultiplier, trip.PackageSlug,
		trip.IdempotencyKey, trip.CreatedAt, trip.UpdatedAt)
	if err != nil {
		return fmt.Errorf("repository: create trip: %w", err)
	}
	return nil
}

func (r *Postgres) Get(ctx context.Context, id string) (*domain.Trip, error) {
	return r.one(ctx, `select `+columns+` from trip where id = $1`, id)
}

func (r *Postgres) FindByIdempotencyKey(ctx context.Context, riderID, key string) (*domain.Trip, error) {
	return r.one(ctx,
		`select `+columns+` from trip where rider_id = $1 and idempotency_key = $2`, riderID, key)
}

func (r *Postgres) Update(ctx context.Context, trip *domain.Trip) error {
	tag, err := r.pool.Exec(ctx, `
		update trip set driver_id = $2, status = $3, total_cents = $4, updated_at = $5
		where id = $1`,
		trip.ID, nullable(trip.DriverID), string(trip.Status), trip.TotalCents, trip.UpdatedAt)
	if err != nil {
		return fmt.Errorf("repository: update trip: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *Postgres) one(ctx context.Context, query string, args ...any) (*domain.Trip, error) {
	var (
		trip     domain.Trip
		driverID *string
		status   string
	)

	err := r.pool.QueryRow(ctx, query, args...).Scan(
		&trip.ID, &trip.RiderID, &driverID, &status,
		&trip.Pickup.Lat, &trip.Pickup.Lng, &trip.Dropoff.Lat, &trip.Dropoff.Lng,
		&trip.Polyline6, &trip.Meters, &trip.Seconds,
		&trip.TotalCents, &trip.SurgeMultiplier, &trip.PackageSlug,
		&trip.IdempotencyKey, &trip.CreatedAt, &trip.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repository: read trip: %w", err)
	}

	trip.Status = domain.Status(status)
	if driverID != nil {
		trip.DriverID = *driverID
	}
	return &trip, nil
}

// nullable keeps an unassigned driver as SQL NULL rather than an empty string,
// so "no driver yet" is expressible in the column rather than by convention.
func nullable(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func (r *Postgres) List(ctx context.Context, filter domain.ListFilter) (domain.Page, error) {
	// One row more than asked for, which is how the presence of a next page is
	// discovered without a second COUNT over the whole history.
	limit := filter.Limit + 1

	// Keyset pagination over (created_at, id), matching the
	// trip_rider_created_at index. An OFFSET would make page 40 scan 40 pages
	// of rows to throw them away, and would shift under a trip created in
	// between.
	query := `
		select ` + columns + ` from trip
		where rider_id = $1
		  and ($2 = '' or status = $2)
		  and ($3 = '' or (created_at, id) < (
		      select created_at, id from trip where id = $3
		  ))
		order by created_at desc, id desc
		limit $4`

	rows, err := r.pool.Query(ctx, query, filter.RiderID, string(filter.Status), filter.Cursor, limit)
	if err != nil {
		return domain.Page{}, fmt.Errorf("repository: list trips: %w", err)
	}
	defer rows.Close()

	var trips []domain.Trip
	for rows.Next() {
		trip, err := scanTrip(rows)
		if err != nil {
			return domain.Page{}, err
		}
		trips = append(trips, *trip)
	}
	if err := rows.Err(); err != nil {
		return domain.Page{}, fmt.Errorf("repository: list trips: %w", err)
	}

	page := domain.Page{}
	if len(trips) == limit {
		trips = trips[:filter.Limit]
		page.NextCursor = trips[len(trips)-1].ID
	}
	page.Trips = trips

	return page, nil
}

// scanner is satisfied by both pgx.Row and pgx.Rows, so one scan function
// serves the single-row reads and the listing.
type scanner interface{ Scan(dest ...any) error }

func scanTrip(row scanner) (*domain.Trip, error) {
	var (
		trip     domain.Trip
		driverID *string
		status   string
	)

	err := row.Scan(
		&trip.ID, &trip.RiderID, &driverID, &status,
		&trip.Pickup.Lat, &trip.Pickup.Lng, &trip.Dropoff.Lat, &trip.Dropoff.Lng,
		&trip.Polyline6, &trip.Meters, &trip.Seconds,
		&trip.TotalCents, &trip.SurgeMultiplier, &trip.PackageSlug,
		&trip.IdempotencyKey, &trip.CreatedAt, &trip.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	trip.Status = domain.Status(status)
	if driverID != nil {
		trip.DriverID = *driverID
	}
	return &trip, nil
}
