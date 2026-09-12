package domain

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"

	// The formats a phone or a browser hands over. Registered here so that
	// what this package accepts is decided in this package.
	_ "image/png"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	// AvatarSize is the side of a stored photo. Enough for the largest place a
	// face is shown, at twice its size on a sharp screen.
	AvatarSize = 512

	// MaxUploadBytes is the largest photo accepted.
	MaxUploadBytes = 5 << 20

	// maxSide guards the decoder. A small file can claim huge dimensions, and
	// decoding it would allocate for every pixel it claims.
	maxSide = 8000

	avatarQuality = 85
)

var (
	ErrNotAnImage = errors.New("domain: not a JPEG, PNG or WebP image")
	ErrTooLarge   = errors.New("domain: the photo is too large")
)

// Avatar is an upload as it is stored: its middle square, scaled down to
// AvatarSize (never up), on white where it was transparent, as a JPEG.
//
// Re-encoding is the point as much as the size. The result is built from
// pixels alone, so nothing the upload carried besides them survives — not the
// camera, and not the place a phone recorded the photo was taken, which for a
// selfie is usually someone's home.
//
// A JPEG's orientation tag is not applied. The apps crop and re-encode before
// sending, which bakes the orientation into the pixels, and a raw phone photo
// sent some other way arrives as the camera stored it.
func Avatar(upload []byte) ([]byte, error) {
	if len(upload) > MaxUploadBytes {
		return nil, ErrTooLarge
	}

	config, format, err := image.DecodeConfig(bytes.NewReader(upload))
	if err != nil {
		return nil, ErrNotAnImage
	}
	switch format {
	case "jpeg", "png", "webp":
	default:
		return nil, ErrNotAnImage
	}
	if config.Width < 1 || config.Height < 1 {
		return nil, ErrNotAnImage
	}
	if config.Width > maxSide || config.Height > maxSide {
		return nil, ErrTooLarge
	}

	source, _, err := image.Decode(bytes.NewReader(upload))
	if err != nil {
		return nil, ErrNotAnImage
	}

	bounds := source.Bounds()
	side := min(bounds.Dx(), bounds.Dy())
	crop := image.Rect(0, 0, side, side).Add(image.Pt(
		bounds.Min.X+(bounds.Dx()-side)/2,
		bounds.Min.Y+(bounds.Dy()-side)/2,
	))
	target := min(side, AvatarSize)

	out := image.NewRGBA(image.Rect(0, 0, target, target))
	draw.Draw(out, out.Bounds(), image.White, image.Point{}, draw.Src)
	draw.CatmullRom.Scale(out, out.Bounds(), source, crop, draw.Over, nil)

	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, out, &jpeg.Options{Quality: avatarQuality}); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}
