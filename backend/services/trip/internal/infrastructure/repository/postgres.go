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
