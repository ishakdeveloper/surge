// Package register reads the Dutch vehicle register.
//
// The RDW publishes the whole kentekenregister as open data: no key, no
// signup, CC-0. One request by plate answers everything onboarding needs —
// what the car is, when its inspection lapses, whether it is registered for
// taxi use, and whether an insurer has a policy against it — so a driver types
// six characters instead of a form, and nobody is asked to photograph a
// registration document that the state already publishes.
package register

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ishakdeveloper/surge/services/fleet/internal/domain"
	"github.com/ishakdeveloper/surge/services/fleet/internal/service"
)

// Endpoint is the register's main dataset: gekentekende voertuigen.
const Endpoint = "https://opendata.rdw.nl/resource/m9d7-ebf2.json"

// timeout bounds a lookup. It sits in front of a driver adding a car, and an
// open data portal having a slow morning should cost them seconds.
const timeout = 6 * time.Second

type RDW struct {
	http     *http.Client
	endpoint string
	// token is Socrata's optional app token. Without one the register throttles
	// by IP against a shared pool, which is fine for one lookup per car and
	// not fine for a benchmark.
	token string
}

func New(endpoint, token string) *RDW {
	if endpoint == "" {
		endpoint = Endpoint
	}
	return &RDW{http: &http.Client{Timeout: timeout}, endpoint: endpoint, token: token}
}

// row is the handful of fields onboarding reads, out of the ninety the
// register publishes.
type row struct {
	Plate            string `json:"kenteken"`
	Make             string `json:"merk"`
	Model            string `json:"handelsbenaming"`
	Colour           string `json:"eerste_kleur"`
	Seats            string `json:"aantal_zitplaatsen"`
	APKExpires       string `json:"vervaldatum_apk_dt"`
	FirstRegistered  string `json:"datum_eerste_toelating_dt"`
	TaxiIndicator    string `json:"taxi_indicator"`
	InsuredIndicator string `json:"wam_verzekerd"`
}

// Lookup asks the register about one plate.
func (r *RDW) Lookup(ctx context.Context, plate string) (domain.Registration, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	address := r.endpoint + "?" + url.Values{"kenteken": {plate}}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return domain.Registration{}, fmt.Errorf("register: request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if r.token != "" {
		request.Header.Set("X-App-Token", r.token)
	}

	response, err := r.http.Do(request)
	if err != nil {
		return domain.Registration{}, fmt.Errorf("register: lookup %s: %w", plate, err)
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return domain.Registration{}, fmt.Errorf("register: read: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		// 429 is the shared throttle pool, and the caller should try again.
		return domain.Registration{}, fmt.Errorf("register: status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	var rows []row
	if err := json.Unmarshal(body, &rows); err != nil {
		return domain.Registration{}, fmt.Errorf("register: decode: %w", err)
	}
	// A plate nobody has registered comes back as an empty list rather than a
	// 404, which is the one thing about this API worth knowing.
	if len(rows) == 0 {
		return domain.Registration{}, service.ErrNoSuchPlate
	}

	return registrationOf(rows[0]), nil
}

func registrationOf(source row) domain.Registration {
	seats, _ := strconv.Atoi(source.Seats)
	return domain.Registration{
		Plate:  source.Plate,
		Make:   title(source.Make),
		Model:  title(source.Model),
		Colour: title(source.Colour),
		Seats:  seats,
		// The register publishes every date twice: YYYYMMDD as a string, and
		// ISO in a `_dt` field. The ISO one is the only one that compares and
		// sorts correctly, so it is the only one read here.
		APKExpiresAt:      timestamp(source.APKExpires),
		FirstRegisteredAt: timestamp(source.FirstRegistered),
		TaxiRegistered:    strings.EqualFold(source.TaxiIndicator, "Ja"),
		Insured:           strings.EqualFold(source.InsuredIndicator, "Ja"),
	}
}

// timestamp reads the register's ISO form, and gives up quietly: a missing
// inspection date is a fact about the car, which the domain then refuses.
func timestamp(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{"2006-01-02T15:04:05.000", "2006-01-02T15:04:05"} {
		if parsed, err := time.ParseInLocation(layout, value, time.UTC); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

// title turns the register's shouting into something a person reads. It holds
// every make in capitals — TOYOTA, VOLKSWAGEN — and a driver's car is not
// shouting at them.
func title(value string) string {
	words := strings.Fields(strings.ToLower(value))
	for i, word := range words {
		runes := []rune(word)
		runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
		words[i] = string(runes)
	}
	return strings.Join(words, " ")
}
