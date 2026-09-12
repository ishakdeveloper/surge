package service_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/core/internal/domain"
	"github.com/ishakdeveloper/surge/services/core/internal/infrastructure/repository"
	"github.com/ishakdeveloper/surge/services/core/internal/infrastructure/storage"
	"github.com/ishakdeveloper/surge/services/core/internal/service"
)

var t0 = time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)

func photo(t *testing.T, shade uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 640, 480))
	for y := range 480 {
		for x := range 640 {
			img.Set(x, y, color.RGBA{R: shade, G: uint8(x), B: uint8(y), A: 255})
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, nil); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return out.Bytes()
}

func setup() (*service.Service, *storage.Memory) {
	store := storage.NewMemory()
	return service.New(repository.NewMemory(), store, func() time.Time { return t0 }), store
}

// Somebody who never filled their profile in has an empty one, not an error:
// "no name yet" is what the other side of the trip should see.
func TestAnUnfilledProfileIsEmpty(t *testing.T) {
	profiles, _ := setup()
	view, err := profiles.Get(context.Background(), "rider-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if view.Profile.UserID != "rider-1" || view.Profile.DisplayName != "" || view.AvatarURL != "" {
		t.Errorf("view = %+v", view)
	}
}

func TestANameIsSetTrimmed(t *testing.T) {
	profiles, _ := setup()
	view, err := profiles.Rename(context.Background(), "rider-1", "  Sanne ")
	if err != nil || view.Profile.DisplayName != "Sanne" {
		t.Fatalf("rename = %+v, %v", view, err)
	}
	if _, err := profiles.Rename(context.Background(), "rider-1", " "); !errors.Is(err, domain.ErrNameEmpty) {
		t.Errorf("an empty name: err = %v", err)
	}
}

// A new photo is stored, linked, and the one it replaced is deleted; the same
// photo sent again stays.
func TestANewPhotoReplacesTheOld(t *testing.T) {
	profiles, store := setup()
	ctx := context.Background()

	first, err := profiles.SetAvatar(ctx, "drv-1", photo(t, 10))
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	firstKey := first.Profile.AvatarKey
	if !strings.HasPrefix(firstKey, "avatars/drv-1/") || !store.Has(firstKey) || first.AvatarURL == "" {
		t.Fatalf("first photo = %+v", first)
	}

	again, err := profiles.SetAvatar(ctx, "drv-1", photo(t, 10))
	if err != nil || again.Profile.AvatarKey != firstKey || !store.Has(firstKey) {
		t.Fatalf("the same photo again moved or lost it: %+v, %v", again, err)
	}

	second, err := profiles.SetAvatar(ctx, "drv-1", photo(t, 200))
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second.Profile.AvatarKey == firstKey || !store.Has(second.Profile.AvatarKey) {
		t.Errorf("second photo = %+v", second)
	}
	if store.Has(firstKey) {
		t.Error("the replaced photo is still stored")
	}
}

func TestARemovedPhotoIsDeleted(t *testing.T) {
	profiles, store := setup()
	ctx := context.Background()
	set, err := profiles.SetAvatar(ctx, "drv-1", photo(t, 10))
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	removed, err := profiles.RemoveAvatar(ctx, "drv-1")
	if err != nil || removed.AvatarURL != "" || removed.Profile.AvatarKey != "" {
		t.Fatalf("remove = %+v, %v", removed, err)
	}
	if store.Has(set.Profile.AvatarKey) {
		t.Error("the removed photo is still stored")
	}
}

// Nothing is stored for an upload that is not a photo.
func TestAnUploadThatIsNotAPhotoStoresNothing(t *testing.T) {
	profiles, store := setup()
	if _, err := profiles.SetAvatar(context.Background(), "drv-1", []byte("not a photo")); !errors.Is(err, domain.ErrNotAnImage) {
		t.Fatalf("err = %v", err)
	}
	if store.Len() != 0 {
		t.Errorf("%d objects stored", store.Len())
	}
}

// An id becomes part of a storage key, so one that could climb out of the
// avatars prefix is refused before anything is written.
func TestAnIDThatCouldEscapeTheKeyIsRefused(t *testing.T) {
	profiles, store := setup()
	if _, err := profiles.SetAvatar(context.Background(), "../x", photo(t, 1)); !errors.Is(err, domain.ErrInvalidUser) {
		t.Fatalf("err = %v", err)
	}
	if store.Len() != 0 {
		t.Errorf("%d objects stored", store.Len())
	}
}
