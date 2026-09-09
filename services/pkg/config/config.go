// Package config reads configuration from the environment, strictly.
//
// The distinction it exists to make: an *unset* variable may fall back to a
// default, but a *set but unparseable* one is always an error. The starter this
// project borrows its layout from got that wrong — its env helper swallowed
// parse failures and returned the fallback, which is how its API gateway ended
// up reading HTTP_ADDR while every manifest set GATEWAY_HTTP_ADDR and nobody
// noticed for fifteen commits. A misconfigured service should refuse to start,
// not start on the wrong port.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// MissingError is returned when a required variable is absent or empty.
type MissingError struct{ Key string }

func (e *MissingError) Error() string {
	return fmt.Sprintf("config: %s is required but not set", e.Key)
}

// InvalidError is returned when a variable is set but cannot be parsed. This is
// never downgraded to a default.
type InvalidError struct {
	Key   string
	Value string
	Want  string
	Err   error
}

func (e *InvalidError) Error() string {
	return fmt.Sprintf("config: %s=%q is not a valid %s: %v", e.Key, e.Value, e.Want, e.Err)
}

func (e *InvalidError) Unwrap() error { return e.Err }

// lookup treats an empty value as unset, so `FOO=` in a .env file means "not
// configured" rather than "configured as the empty string".
func lookup(key string) (string, bool) {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return "", false
	}
	return value, true
}

// String returns a required string.
func String(key string) (string, error) {
	value, ok := lookup(key)
	if !ok {
		return "", &MissingError{Key: key}
	}
	return value, nil
}

// StringOr returns the variable, or fallback when it is unset.
func StringOr(key, fallback string) string {
	if value, ok := lookup(key); ok {
		return value
	}
	return fallback
}

// Strings splits a comma-separated list, trimming each entry and dropping
// blanks. Returns fallback when unset.
func Strings(key string, fallback []string) []string {
	value, ok := lookup(key)
	if !ok {
		return fallback
	}

	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// Int returns a required integer.
func Int(key string) (int, error) {
	value, ok := lookup(key)
	if !ok {
		return 0, &MissingError{Key: key}
	}
	return parseInt(key, value)
}

// IntOr returns the parsed variable, or fallback when it is unset. A set but
// unparseable value is an error, never the fallback.
func IntOr(key string, fallback int) (int, error) {
	value, ok := lookup(key)
	if !ok {
		return fallback, nil
	}
	return parseInt(key, value)
}

func parseInt(key, value string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, &InvalidError{Key: key, Value: value, Want: "integer", Err: err}
	}
	return parsed, nil
}

// BoolOr accepts the forms strconv.ParseBool does: 1, t, T, TRUE, true, True
// and their false counterparts.
func BoolOr(key string, fallback bool) (bool, error) {
	value, ok := lookup(key)
	if !ok {
		return fallback, nil
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, &InvalidError{Key: key, Value: value, Want: "boolean", Err: err}
	}
	return parsed, nil
}

// DurationOr parses a Go duration string such as "4s" or "250ms".
func DurationOr(key string, fallback time.Duration) (time.Duration, error) {
	value, ok := lookup(key)
	if !ok {
		return fallback, nil
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, &InvalidError{Key: key, Value: value, Want: "duration", Err: err}
	}
	return parsed, nil
}
