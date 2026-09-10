// Command wire-fixtures writes the server-direction WebSocket fixtures that
// `shared/wire/wire_test.go` and `packages/domain/test/realtime/Wire.test.ts`
// both assert against.
//
// Generated rather than hand-written because these are what Go actually emits,
// including the details nobody would think to write down — the `_tag` that
// appears twice in an offer envelope, once on the wrapper and once on the
// payload, is a property of `encoding/json` over these structs rather than a
// decision, and a hand-made fixture would quietly omit it and let the
// TypeScript schema reject the real thing.
//
// The client-direction fixtures are not generated here: they are what the
// browser emits, and Go's view of them is `wire.ClientMessage`, which is the
// thing under test.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ishakdeveloper/surge/shared/wire"
)

const dir = "shared/wire/testdata"

func main() {
	fixtures := map[string]any{
		"server_welcome.json": wire.ServerMessage{Tag: wire.TagServerWelcome},
		"server_error.json":   wire.ServerMessage{Tag: wire.TagServerError, Error: "token expired"},
		"server_trip_updated.json": wire.ServerMessage{Tag: wire.TagTripUpdated, Trip: &wire.TripUpdate{
			TripID:   "0f2a6c1e-9d4b-4a77-8c31-6b1e5a2d9f80",
			RiderID:  "rider-000456",
			DriverID: "drv-000123",
			Status:   "TRIP_STATUS_ACCEPTED",
			AtMs:     1757512331000,
		}},
		"server_offer.json": wire.ServerMessage{Tag: wire.TagOffer, Offer: &wire.Offer{
			Tag:            wire.TagOffer,
			TripID:         "0f2a6c1e-9d4b-4a77-8c31-6b1e5a2d9f80",
			DriverID:       "drv-000123",
			RiderID:        "rider-000456",
			PickupLat:      52.3702,
			PickupLng:      4.8952,
			ReplyCell:      "871f1d492ffffff",
			ExpiresAtMs:    1757512345000,
			DispatchedAtMs: 1757512330000,
			RequestedAtMs:  1757512329412,
		}},
	}

	for name, value := range fixtures {
		raw, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "marshal %s: %v\n", name, err)
			os.Exit(1)
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "write %s: %v\n", path, err)
			os.Exit(1)
		}
		fmt.Println(path)
	}
}
