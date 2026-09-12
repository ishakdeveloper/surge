// Package fake is the fleet's external world on a laptop: a register that
// answers from a table, an identity provider that verifies whoever asks, and a
// reader that reads nothing.
//
// What `make dev-fleet` runs on, and what the tests use, so neither needs a
// Stripe key, an Anthropic key or the open internet.
package fake

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ishakdeveloper/surge/services/fleet/internal/domain"
	"github.com/ishakdeveloper/surge/services/fleet/internal/service"
)

// Register answers from a small table of plates.
//
// The plates are real ones from the RDW's own open data, with the values it
// returns for them, so what a developer sees locally is shaped like what
// Amsterdam returns.
type Register struct {
	mu    sync.Mutex
	plate map[string]domain.Registration
}

func NewRegister(now time.Time) *Register {
	year := func(years int) time.Time { return now.AddDate(years, 0, 0) }
	return &Register{plate: map[string]domain.Registration{
		// A taxi-registered, insured Prius: the happy path.
		"02JLT3": {
			Plate: "02JLT3", Make: "Toyota", Model: "Toyota Prius", Colour: "Wit", Seats: 5,
			APKExpiresAt: year(1), FirstRegisteredAt: now.AddDate(-8, 0, 0),
			TaxiRegistered: true, Insured: true,
		},
		// Six seats, for Surge XL.
		"14BKN9": {
			Plate: "14BKN9", Make: "Volkswagen", Model: "Volkswagen Caddy", Colour: "Grijs", Seats: 6,
			APKExpiresAt: year(1), FirstRegisteredAt: now.AddDate(-3, 0, 0),
			TaxiRegistered: true, Insured: true,
		},
		// Its inspection lapsed last month: refused, with the date.
		"73RXV1": {
			Plate: "73RXV1", Make: "Opel", Model: "Opel Astra", Colour: "Blauw", Seats: 5,
			APKExpiresAt: now.AddDate(0, -1, 0), FirstRegisteredAt: now.AddDate(-11, 0, 0),
			TaxiRegistered: false, Insured: true,
		},
		// Nobody insures it: refused.
		"58GDP2": {
			Plate: "58GDP2", Make: "Renault", Model: "Renault Clio", Colour: "Rood", Seats: 5,
			APKExpiresAt: year(2), FirstRegisteredAt: now.AddDate(-5, 0, 0),
			TaxiRegistered: false, Insured: false,
		},
		// Insured and inspected, but not down for taxi use: allowed, and the
		// reviewer is told.
		"91HZK7": {
			Plate: "91HZK7", Make: "Skoda", Model: "Skoda Octavia", Colour: "Zwart", Seats: 5,
			APKExpiresAt: year(1), FirstRegisteredAt: now.AddDate(-2, 0, 0),
			TaxiRegistered: false, Insured: true,
		},
	}}
}

func (r *Register) Lookup(_ context.Context, plate string) (domain.Registration, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	registration, ok := r.plate[plate]
	if !ok {
		return domain.Registration{}, service.ErrNoSuchPlate
	}
	return registration, nil
}

// Identity verifies whoever asks, immediately.
//
// It returns a link to a page that does not exist, because nothing local
// should open a real verification flow; the verdict is delivered by calling
// Verdicts, which is what the tests and `make fleet-verify` do.
type Identity struct {
	mu       sync.Mutex
	sessions map[string]string
	verdicts []service.Verdict
}

func NewIdentity() *Identity {
	return &Identity{sessions: make(map[string]string)}
}

func (i *Identity) Start(_ context.Context, driverID, _ string) (service.Session, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	id := fmt.Sprintf("vs_fake_%s_%d", driverID, len(i.sessions)+1)
	i.sessions[id] = driverID
	return service.Session{ID: id, URL: "https://verify.invalid/" + id}, nil
}

// Verdict is what the provider would have said, for a test to hand to the
// service.
func (i *Identity) Verdict(sessionID string, at time.Time) service.Verdict {
	i.mu.Lock()
	defer i.mu.Unlock()

	return service.Verdict{
		SessionID:        sessionID,
		Status:           domain.IdentityVerified,
		Name:             "Jan de Vries",
		Document:         "5" + strings.ToUpper(sessionID[len(sessionID)-4:]),
		LicenceExpiresAt: at.AddDate(5, 0, 0),
	}
}

// Reader reads nothing, and says so.
//
// A failed reading is a state the reviewer path already handles — they read
// the document themselves — so the fake takes the honest branch rather than
// inventing an insurer.
type Reader struct{}

func NewReader() Reader { return Reader{} }

func (Reader) Read(_ context.Context, _, _ string) (domain.Extraction, error) {
	return domain.Extraction{Status: domain.ExtractionNone}, nil
}
