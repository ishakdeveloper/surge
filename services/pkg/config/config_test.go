package config_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/pkg/config"
)

// The property this package exists for.
//
// An unset variable may fall back to a default. A variable that is *set* to
// something unparseable must never fall back — that is how a service ends up
// running on a port nobody asked for, which is exactly the bug in the starter
// this layout came from.
func TestSetButUnparseableIsAnErrorNotAFallback(t *testing.T) {
	t.Setenv("SURGE_PORT", "not-a-number")
	t.Setenv("SURGE_FLAG", "yes-please")
	t.Setenv("SURGE_EVERY", "4 seconds")

	if _, err := config.IntOr("SURGE_PORT", 8100); err == nil {
		t.Error("IntOr returned the fallback for an unparseable value")
	}
	if _, err := config.BoolOr("SURGE_FLAG", false); err == nil {
		t.Error("BoolOr returned the fallback for an unparseable value")
	}
	if _, err := config.DurationOr("SURGE_EVERY", time.Second); err == nil {
		t.Error("DurationOr returned the fallback for an unparseable value")
	}

	var invalid *config.InvalidError
	if _, err := config.IntOr("SURGE_PORT", 8100); !errors.As(err, &invalid) {
		t.Fatalf("want *InvalidError, got %T", err)
	}
	if invalid.Key != "SURGE_PORT" || invalid.Value != "not-a-number" {
		t.Errorf("error should name the offending key and value, got %v", invalid)
	}
}

func TestUnsetFallsBack(t *testing.T) {
	port, err := config.IntOr("SURGE_DEFINITELY_UNSET", 8100)
	if err != nil || port != 8100 {
		t.Errorf("got (%d, %v), want (8100, nil)", port, err)
	}

	if got := config.StringOr("SURGE_DEFINITELY_UNSET", "fallback"); got != "fallback" {
		t.Errorf("got %q, want %q", got, "fallback")
	}
}

// An empty value means "not configured", not "configured as empty". A .env file
// full of `FOO=` placeholders is the normal state of a fresh clone, and every
// one of them should take its default rather than an empty string.
func TestEmptyCountsAsUnset(t *testing.T) {
	t.Setenv("SURGE_BLANK", "")

	if got := config.StringOr("SURGE_BLANK", "fallback"); got != "fallback" {
		t.Errorf("got %q, want %q", got, "fallback")
	}

	var missing *config.MissingError
	if _, err := config.String("SURGE_BLANK"); !errors.As(err, &missing) {
		t.Errorf("want *MissingError, got %T", err)
	}
}

func TestStrings(t *testing.T) {
	t.Setenv("SURGE_BROKERS", " localhost:19092 , localhost:19093 ,, ")

	got := config.Strings("SURGE_BROKERS", nil)
	want := []string{"localhost:19092", "localhost:19093"}

	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got %v, want %v", got, want)
		}
	}
}
