// Package service is core's logic.
//
// It depends on domain interfaces and nothing else, so its tests run against
// in-memory implementations and finish in milliseconds.
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/ishakdeveloper/surge/services/core/internal/domain"
)

// Repository keeps profiles.
type Repository interface {
	// Get is a profile, or an empty one for somebody who has not made one.
	Get(ctx context.Context, userID string) (domain.Profile, error)
	SaveName(ctx context.Context, userID, name string, at time.Time) (domain.Profile, error)
	// SaveAvatar sets the photo's key, "" for none, and returns the key it
	// replaced, "" if there was none.
	SaveAvatar(ctx context.Context, userID, key string, at time.Time) (domain.Profile, string, error)
}

// Store holds photos. Only this service has its credentials.
type Store interface {
	Put(ctx context.Context, key string, body []byte, contentType string) error
	// Delete removes a photo. One that is already gone is not an error.
	Delete(ctx context.Context, key string) error
	// URL is a link to a photo, good from now for at least an hour.
	URL(key string, now time.Time) (string, error)
}

// View is a profile as it is shown: its photo as a link rather than a key.
type View struct {
	Profile   domain.Profile
	AvatarURL string
}

type Service struct {
	repository Repository
	store      Store
	now        func() time.Time
}

func New(repository Repository, store Store, now func() time.Time) *Service {
	return &Service{repository: repository, store: store, now: now}
}

// Get is someone's profile, empty where they have not filled it in.
func (s *Service) Get(ctx context.Context, userID string) (View, error) {
	if !domain.ValidUserID(userID) {
		return View{}, domain.ErrInvalidUser
	}
	profile, err := s.repository.Get(ctx, userID)
	if err != nil {
		return View{}, err
	}
	return s.view(profile)
}

// Rename sets the caller's first name.
func (s *Service) Rename(ctx context.Context, userID, raw string) (View, error) {
	if !domain.ValidUserID(userID) {
		return View{}, domain.ErrInvalidUser
	}
	name, err := domain.NormalizeName(raw)
	if err != nil {
		return View{}, err
	}
	profile, err := s.repository.SaveName(ctx, userID, name, s.now())
	if err != nil {
		return View{}, err
	}
	return s.view(profile)
}

// SetAvatar makes an upload the caller's photo, and deletes the one it
// replaces.
//
// Stored before it is recorded: a photo recorded but not stored would be a
// broken image on everyone's screen, while one stored but not recorded is only
// an object nobody links to.
func (s *Service) SetAvatar(ctx context.Context, userID string, upload []byte) (View, error) {
	if !domain.ValidUserID(userID) {
		return View{}, domain.ErrInvalidUser
	}
	photo, err := domain.Avatar(upload)
	if err != nil {
		return View{}, err
	}
	sum := sha256.Sum256(photo)
	key := fmt.Sprintf("avatars/%s/%s.jpg", userID, hex.EncodeToString(sum[:8]))

	if err := s.store.Put(ctx, key, photo, "image/jpeg"); err != nil {
		return View{}, fmt.Errorf("service: store the photo: %w", err)
	}
	profile, previous, err := s.repository.SaveAvatar(ctx, userID, key, s.now())
	if err != nil {
		return View{}, err
	}
	s.forget(ctx, previous, key)
	return s.view(profile)
}

// RemoveAvatar deletes the caller's photo.
func (s *Service) RemoveAvatar(ctx context.Context, userID string) (View, error) {
	if !domain.ValidUserID(userID) {
		return View{}, domain.ErrInvalidUser
	}
	profile, previous, err := s.repository.SaveAvatar(ctx, userID, "", s.now())
	if err != nil {
		return View{}, err
	}
	s.forget(ctx, previous, "")
	return s.view(profile)
}

// forget deletes a replaced photo. The same photo sent twice has the same key,
// and is not deleted from under itself. A delete that fails leaves an object
// nothing links to, which is logged rather than failing the change the person
// asked for.
func (s *Service) forget(ctx context.Context, previous, current string) {
	if previous == "" || previous == current {
		return
	}
	if err := s.store.Delete(ctx, previous); err != nil {
		slog.Warn("could not delete a replaced photo", "key", previous, "error", err)
	}
}

func (s *Service) view(profile domain.Profile) (View, error) {
	if profile.AvatarKey == "" {
		return View{Profile: profile}, nil
	}
	url, err := s.store.URL(profile.AvatarKey, s.now())
	if err != nil {
		return View{}, fmt.Errorf("service: link the photo: %w", err)
	}
	return View{Profile: profile, AvatarURL: url}, nil
}
