package domain_test

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"github.com/ishakdeveloper/surge/services/core/internal/domain"
)

func encodeJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 120, A: 255})
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, nil); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return out.Bytes()
}

func decode(t *testing.T, photo []byte) image.Image {
	t.Helper()
	img, format, err := image.Decode(bytes.NewReader(photo))
	if err != nil || format != "jpeg" {
		t.Fatalf("the stored photo is not a JPEG: %v (%s)", err, format)
	}
	return img
}

// A landscape photo keeps its middle square, scaled to the stored size.
func TestAnAvatarIsTheMiddleSquare(t *testing.T) {
	photo, err := domain.Avatar(encodeJPEG(t, 1200, 800))
	if err != nil {
		t.Fatalf("avatar: %v", err)
	}
	if bounds := decode(t, photo).Bounds(); bounds.Dx() != domain.AvatarSize || bounds.Dy() != domain.AvatarSize {
		t.Errorf("stored at %v, want %d square", bounds, domain.AvatarSize)
	}
}

// A small photo is cropped but not blown up into a blur.
func TestASmallPhotoIsNotEnlarged(t *testing.T) {
	photo, err := domain.Avatar(encodeJPEG(t, 200, 300))
	if err != nil {
		t.Fatalf("avatar: %v", err)
	}
	if bounds := decode(t, photo).Bounds(); bounds.Dx() != 200 || bounds.Dy() != 200 {
		t.Errorf("stored at %v, want 200 square", bounds)
	}
}

// What a phone writes into a photo — the place it was taken among it — does
// not survive: the stored photo is made from pixels alone.
func TestAnAvatarDropsWhatThePhotoCarried(t *testing.T) {
	plain := encodeJPEG(t, 300, 300)
	payload := []byte("Exif\x00\x00GPSLatitude 52.3702 GPSLongitude 4.8952")
	segment := append([]byte{0xFF, 0xE1, byte((len(payload) + 2) >> 8), byte(len(payload) + 2)}, payload...)
	tagged := append(append(append([]byte{}, plain[:2]...), segment...), plain[2:]...)

	photo, err := domain.Avatar(tagged)
	if err != nil {
		t.Fatalf("avatar: %v", err)
	}
	for _, trace := range []string{"Exif", "GPS", "52.3702"} {
		if bytes.Contains(photo, []byte(trace)) {
			t.Errorf("the stored photo still carries %q", trace)
		}
	}
}

// Transparent parts sit on white, not on the black a JPEG would otherwise give
// them.
func TestATransparentPhotoIsOnWhite(t *testing.T) {
	var clear bytes.Buffer
	if err := png.Encode(&clear, image.NewNRGBA(image.Rect(0, 0, 64, 64))); err != nil {
		t.Fatalf("encode: %v", err)
	}
	photo, err := domain.Avatar(clear.Bytes())
	if err != nil {
		t.Fatalf("avatar: %v", err)
	}
	r, g, b, _ := decode(t, photo).At(32, 32).RGBA()
	if r>>8 < 240 || g>>8 < 240 || b>>8 < 240 {
		t.Errorf("a transparent pixel became (%d, %d, %d), want white", r>>8, g>>8, b>>8)
	}
}

func TestAnythingButAPhotoIsRefused(t *testing.T) {
	var animation bytes.Buffer
	if err := gif.Encode(&animation, image.NewPaletted(image.Rect(0, 0, 8, 8), color.Palette{color.Black}), nil); err != nil {
		t.Fatalf("encode: %v", err)
	}
	for name, upload := range map[string][]byte{
		"svg":   []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`),
		"gif":   animation.Bytes(),
		"empty": {},
	} {
		if _, err := domain.Avatar(upload); !errors.Is(err, domain.ErrNotAnImage) {
			t.Errorf("%s: err = %v, want ErrNotAnImage", name, err)
		}
	}
}

func TestAPhotoTooLargeIsRefused(t *testing.T) {
	if _, err := domain.Avatar(make([]byte, domain.MaxUploadBytes+1)); !errors.Is(err, domain.ErrTooLarge) {
		t.Errorf("an oversized upload: err = %v, want ErrTooLarge", err)
	}

	// Small on disk, enormous once decoded: refused before a pixel is allocated.
	var wide bytes.Buffer
	if err := png.Encode(&wide, image.NewGray(image.Rect(0, 0, 8001, 1))); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, err := domain.Avatar(wide.Bytes()); !errors.Is(err, domain.ErrTooLarge) {
		t.Errorf("8001 pixels wide: err = %v, want ErrTooLarge", err)
	}
}

func TestANameIsKeptTrimmed(t *testing.T) {
	cases := []struct {
		raw  string
		want string
		err  error
	}{
		{"  Sanne ", "Sanne", nil},
		{"Anne   Marie", "Anne Marie", nil},
		{"   ", "", domain.ErrNameEmpty},
		{strings.Repeat("a", domain.MaxNameLength+1), "", domain.ErrNameTooLong},
		{"Sa\x00nne", "", domain.ErrNameInvalid},
	}
	for _, c := range cases {
		got, err := domain.NormalizeName(c.raw)
		if got != c.want || !errors.Is(err, c.err) {
			t.Errorf("NormalizeName(%q) = %q, %v; want %q, %v", c.raw, got, err, c.want, c.err)
		}
	}
}

func TestAUserIDIsSafeInAStorageKey(t *testing.T) {
	for id, want := range map[string]bool{
		"Xq3K9mZ2": true, "drv-000123": true, "": false, "../../etc": false, "a/b": false,
	} {
		if got := domain.ValidUserID(id); got != want {
			t.Errorf("ValidUserID(%q) = %v", id, got)
		}
	}
}
