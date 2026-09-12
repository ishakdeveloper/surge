package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/core/internal/infrastructure/repository"
	"github.com/ishakdeveloper/surge/shared/pgtest"
)

var t0 = time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)

func TestAProfileIsEmptyUntilFilledIn(t *testing.T) {
	repo := repository.NewPostgres(pgtest.Pool(t))
	profile, err := repo.Get(context.Background(), "rider-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if profile.UserID != "rider-1" || profile.DisplayName != "" || profile.AvatarKey != "" {
		t.Errorf("profile = %+v", profile)
	}
}

// The name and the photo are saved separately and neither overwrites the
// other; saving a photo says which one it replaced.
func TestNameAndPhotoAreKeptTogether(t *testing.T) {
	repo := repository.NewPostgres(pgtest.Pool(t))
	ctx := context.Background()

	if _, err := repo.SaveName(ctx, "drv-1", "Sanne", t0); err != nil {
		t.Fatalf("name: %v", err)
	}
	withPhoto, previous, err := repo.SaveAvatar(ctx, "drv-1", "avatars/drv-1/a.jpg", t0.Add(time.Minute))
	if err != nil {
		t.Fatalf("photo: %v", err)
	}
	if previous != "" || withPhoto.DisplayName != "Sanne" || withPhoto.AvatarKey != "avatars/drv-1/a.jpg" {
		t.Fatalf("after the first photo: %+v, previous %q", withPhoto, previous)
	}

	_, previous, err = repo.SaveAvatar(ctx, "drv-1", "avatars/drv-1/b.jpg", t0.Add(2*time.Minute))
	if err != nil || previous != "avatars/drv-1/a.jpg" {
		t.Fatalf("replacing it reported %q, %v", previous, err)
	}

	renamed, err := repo.SaveName(ctx, "drv-1", "Sanne V", t0.Add(3*time.Minute))
	if err != nil || renamed.AvatarKey != "avatars/drv-1/b.jpg" {
		t.Fatalf("renaming lost the photo: %+v, %v", renamed, err)
	}

	stored, err := repo.Get(ctx, "drv-1")
	if err != nil || stored.DisplayName != "Sanne V" || stored.AvatarKey != "avatars/drv-1/b.jpg" {
		t.Errorf("stored = %+v, %v", stored, err)
	}
}

// A photo saved before any name makes the row, and removing it leaves the name.
func TestAPhotoCanComeFirstAndGo(t *testing.T) {
	repo := repository.NewPostgres(pgtest.Pool(t))
	ctx := context.Background()

	if _, _, err := repo.SaveAvatar(ctx, "rider-2", "avatars/rider-2/a.jpg", t0); err != nil {
		t.Fatalf("photo: %v", err)
	}
	if _, err := repo.SaveName(ctx, "rider-2", "Joris", t0); err != nil {
		t.Fatalf("name: %v", err)
	}
	removed, previous, err := repo.SaveAvatar(ctx, "rider-2", "", t0)
	if err != nil || previous != "avatars/rider-2/a.jpg" || removed.AvatarKey != "" || removed.DisplayName != "Joris" {
		t.Errorf("removed = %+v, previous %q, %v", removed, previous, err)
	}
}
