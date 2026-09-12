// Package domain is core's rules, expressed without reference to how they are
// stored or transported.
//
// Nothing here imports gRPC or Postgres. That is what lets the part worth
// being sure about — what a name may be, what becomes of a photo — be tested by
// calling functions.
package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Profile is who someone is to the people they ride with: a first name and a
// photo.
type Profile struct {
	UserID      string
	DisplayName string
	// AvatarKey is where the photo is stored, empty without one. It is derived
	// from the photo's bytes, so a new photo is a new key.
	AvatarKey string
	UpdatedAt time.Time
}

// MaxNameLength is long enough for "Anne-Marie" and short enough for the line
// a driver's name gets beside their photo.
const MaxNameLength = 40

var (
	ErrNameEmpty   = errors.New("domain: the name is empty")
	ErrNameTooLong = errors.New("domain: the name is too long")
	ErrNameInvalid = errors.New("domain: the name has characters that cannot be shown")
	ErrInvalidUser = errors.New("domain: not a user id")
)

// NormalizeName is a name as it is kept: trimmed, with any run of spaces
// inside it made one.
func NormalizeName(raw string) (string, error) {
	name := strings.Join(strings.Fields(raw), " ")
	if name == "" {
		return "", ErrNameEmpty
	}
	if utf8.RuneCountInString(name) > MaxNameLength {
		return "", ErrNameTooLong
	}
	for _, r := range name {
		if unicode.IsControl(r) || !unicode.IsPrint(r) {
			return "", ErrNameInvalid
		}
	}
	return name, nil
}

// userID is what an id may be, since it becomes part of a storage key: the
// auth service's ids and the simulator's are letters, digits, dashes and
// underscores.
var userID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// ValidUserID reports whether id may name a profile.
func ValidUserID(id string) bool { return userID.MatchString(id) }
