package register_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/fleet/internal/infrastructure/register"
	"github.com/ishakdeveloper/surge/services/fleet/internal/service"
)

// The register's own shape, field for field, as opendata.rdw.nl returns it for
// a plate. Copied rather than invented: the two date spellings and the Dutch
// Ja/Nee are exactly the details a hand-made fixture would quietly get wrong.
const answer = `[{
  "kenteken": "02JLT3",
  "voertuigsoort": "Personenauto",
  "merk": "TOYOTA",
  "handelsbenaming": "TOYOTA PRIUS",
  "eerste_kleur": "WIT",
  "tweede_kleur": "Niet geregistreerd",
  "datum_eerste_toelating": "20090706",
  "datum_eerste_toelating_dt": "2009-07-06T00:00:00.000",
  "vervaldatum_apk": "20271011",
  "vervaldatum_apk_dt": "2027-10-11T00:00:00.000",
  "taxi_indicator": "Ja",
  "wam_verzekerd": "Ja",
  "aantal_zitplaatsen": "5"
}]`

func TestLookup(t *testing.T) {
	var asked string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Query().Get("kenteken")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(answer))
	}))
	defer server.Close()

	registration, err := register.New(server.URL, "").Lookup(context.Background(), "02JLT3")
	if err != nil {
		t.Fatal(err)
	}
	if asked != "02JLT3" {
		t.Errorf("asked about %q", asked)
	}

	// The register shouts; a driver's car should not shout back at them.
	if registration.Make != "Toyota" || registration.Model != "Toyota Prius" || registration.Colour != "Wit" {
		t.Errorf("read %+v", registration)
	}
	if registration.Seats != 5 {
		t.Errorf("seats %d — the register sends them as a string", registration.Seats)
	}
	// The ISO field, not the YYYYMMDD one: only one of them sorts.
	if want := time.Date(2027, 10, 11, 0, 0, 0, 0, time.UTC); !registration.APKExpiresAt.Equal(want) {
		t.Errorf("APK expires %v, want %v", registration.APKExpiresAt, want)
	}
	if !registration.TaxiRegistered || !registration.Insured {
		t.Errorf("Ja read as false: %+v", registration)
	}
}

// A plate nobody has registered comes back as an empty list rather than a 404,
// which is the one thing about this API worth knowing.
func TestAnUnknownPlate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	_, err := register.New(server.URL, "").Lookup(context.Background(), "99ZZZ9")
	if !errors.Is(err, service.ErrNoSuchPlate) {
		t.Errorf("an empty list became %v", err)
	}
}

// Without an app token the register throttles by IP against a shared pool, and
// says so with a 429. That is worth retrying, not worth reporting as "no such
// car".
func TestThrottlingIsNotAMissingCar(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
	}))
	defer server.Close()

	_, err := register.New(server.URL, "").Lookup(context.Background(), "02JLT3")
	if err == nil || errors.Is(err, service.ErrNoSuchPlate) {
		t.Errorf("a throttle became %v", err)
	}
}

func TestTheAppTokenTravelsAsAHeader(t *testing.T) {
	var token string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token = r.Header.Get("X-App-Token")
		_, _ = w.Write([]byte(answer))
	}))
	defer server.Close()

	if _, err := register.New(server.URL, "a-token").Lookup(context.Background(), "02JLT3"); err != nil {
		t.Fatal(err)
	}
	if token != "a-token" {
		t.Errorf("token sent as %q", token)
	}
}
