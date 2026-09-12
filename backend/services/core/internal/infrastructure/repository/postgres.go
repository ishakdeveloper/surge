package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ishakdeveloper/surge/services/core/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres keeps profiles in core_profile, one row per person who has filled
// something in.
type Postgres struct {
	pool *pgxpool.Pool
}

func NewPostgres(pool *pgxpool.Pool) *Postgres { return &Postgres{pool: pool} }

func (p *Postgres) Get(ctx context.Context, userID string) (domain.Profile, error) {
	profile := domain.Profile{UserID: userID}
	err := p.pool.QueryRow(ctx,
		`select display_name, avatar_key, updated_at from core_profile where user_id = $1`, userID,
	).Scan(&profile.DisplayName, &profile.AvatarKey, &profile.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Profile{UserID: userID}, nil
	}
	if err != nil {
		return domain.Profile{}, fmt.Errorf("repository: read the profile of %s: %w", userID, err)
	}
	return profile, nil
}

func (p *Postgres) SaveName(ctx context.Context, userID, name string, at time.Time) (domain.Profile, error) {
	profile := domain.Profile{UserID: userID}
	err := p.pool.QueryRow(ctx, `
		insert into core_profile (user_id, display_name, updated_at) values ($1, $2, $3)
		on conflict (user_id) do update set display_name = excluded.display_name, updated_at = excluded.updated_at
		returning display_name, avatar_key, updated_at`,
		userID, name, at,
	).Scan(&profile.DisplayName, &profile.AvatarKey, &profile.UpdatedAt)
	if err != nil {
		return domain.Profile{}, fmt.Errorf("repository: save the name of %s: %w", userID, err)
	}
	return profile, nil
}

// SaveAvatar reads the key it replaces in the same statement that replaces it,
// from the snapshot the statement started with. Two uploads racing can each
// see the same previous key, and the loser's own photo is then an object
// nothing links to — which costs storage, never a broken image.
func (p *Postgres) SaveAvatar(ctx context.Context, userID, key string, at time.Time) (domain.Profile, string, error) {
	profile := domain.Profile{UserID: userID}
	var previous string
	err := p.pool.QueryRow(ctx, `
		with previous as (select avatar_key from core_profile where user_id = $1)
		insert into core_profile (user_id, avatar_key, updated_at) values ($1, $2, $3)
		on conflict (user_id) do update set avatar_key = excluded.avatar_key, updated_at = excluded.updated_at
		returning display_name, avatar_key, updated_at, coalesce((select avatar_key from previous), '')`,
		userID, key, at,
	).Scan(&profile.DisplayName, &profile.AvatarKey, &profile.UpdatedAt, &previous)
	if err != nil {
		return domain.Profile{}, "", fmt.Errorf("repository: save the photo of %s: %w", userID, err)
	}
	return profile, previous, nil
}
