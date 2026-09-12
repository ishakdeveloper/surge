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
		"server_fleet_cells.json": wire.ServerMessage{Tag: wire.TagFleetUpdate, Fleet: &wire.FleetUpdate{
			AtMs: 1757512340000, Mode: "cells",
			Cells: []wire.FleetCell{{Cell: "871f1d492ffffff", Drivers: 12, Idle: 9, Multiplier: 1.4, Boundary: [][2]float64{
				{4.8943, 52.3702}, {4.9012, 52.3751}, {4.9101, 52.3733}, {4.9121, 52.3660}, {4.9052, 52.3611}, {4.8963, 52.3629},
			}}},
			Drivers: []wire.FleetDriver{},
			Shards:  []wire.FleetShard{{Partition: 7, Instance: "matcher-a", Drivers: 12, Pending: 1, Offers: 2, AgeMs: 340}},
			Stats: wire.FleetStats{Drivers: 12, Idle: 9, Pending: 1, Offers: 2, MatchedPerSecond: 0.5,
				P50Ms: 2100, P95Ms: 3900, P99Ms: 4400, MaxMultiplier: 1.4, SurgingCells: 1},
		}},
		"server_fleet_drivers.json": wire.ServerMessage{Tag: wire.TagFleetUpdate, Fleet: &wire.FleetUpdate{
			AtMs: 1757512340000, Mode: "drivers",
			Cells: []wire.FleetCell{},
			Drivers: []wire.FleetDriver{{ID: "drv-000123", Lat: 52.3702, Lng: 4.8952, Heading: 137.5,
				Status: wire.StatusIdle, Cell: "871f1d492ffffff", Reserved: false}},
			Shards: []wire.FleetShard{},
			Stats:  wire.FleetStats{Drivers: 1, Idle: 1, MaxMultiplier: 1},
		}},
		"server_driver_position.json": wire.ServerMessage{Tag: wire.TagDriverPosition, Position: &wire.DriverPosition{
			TripID: "0f2a6c1e-9d4b-4a77-8c31-6b1e5a2d9f80", DriverID: "drv-000123",
			Lat: 52.3711, Lng: 4.8963, Heading: 90, AtMs: 1757512341000, EtaSeconds: 240,
		}},
		"server_city.json": wire.ServerMessage{Tag: wire.TagCityUpdate, City: &wire.CityUpdate{
			AtMs:     1757512340000,
			Cars:     []wire.CityCar{{Key: "5f1c09a2e4b7d833", Lat: 52.3702, Lng: 4.8952, Heading: 137.5}},
			Bookings: []wire.CityBooking{{Key: "a93e61d0c2f84b17", Lat: 52.3687, Lng: 4.8931, AtMs: 1757512338000}},
		}},
		"server_trip_updated.json": wire.ServerMessage{Tag: wire.TagTripUpdated, Trip: &wire.TripUpdate{
			TripID:   "0f2a6c1e-9d4b-4a77-8c31-6b1e5a2d9f80",
			RiderID:  "rider-000456",
			DriverID: "drv-000123",
			Status:   "TRIP_STATUS_ACCEPTED",
			AtMs:     1757512331000,
		}},
		"server_payments_changed.json": wire.ServerMessage{Tag: wire.TagPaymentsChanged, Payments: &wire.PaymentsChange{
			TripID: "0f2a6c1e-9d4b-4a77-8c31-6b1e5a2d9f80",
			AtMs:   1757512345000,
		}},
		"server_chat_changed.json": wire.ServerMessage{Tag: wire.TagChatChanged, Chat: &wire.ChatChange{
			ConversationID: "5b1d7e2a-3c4f-4e8a-9b6d-2f7c1a0e8d43",
			Kind:           "trip",
			TripID:         "0f2a6c1e-9d4b-4a77-8c31-6b1e5a2d9f80",
			LastSeq:        7,
			AtMs:           1757512350000,
		}},
		"server_chat_read.json": wire.ServerMessage{Tag: wire.TagChatRead, ChatRead: &wire.ChatRead{
			ConversationID: "5b1d7e2a-3c4f-4e8a-9b6d-2f7c1a0e8d43",
			UserID:         "drv-000123",
			Seq:            7,
			AtMs:           1757512352000,
		}},
		"server_chat_typing.json": wire.ServerMessage{Tag: wire.TagChatTyping, ChatTyping: &wire.ChatTyping{
			ConversationID: "5b1d7e2a-3c4f-4e8a-9b6d-2f7c1a0e8d43",
			UserID:         "rider-000456",
			AtMs:           1757512351000,
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

	// One `geo.events` record per variant. Go writes and reads these, so no
	// second language holds them honest; time does. During every rollout an old
	// matcher reads what a new ingest wrote, and CI decodes the fixtures on
	// `main` with a branch's code to prove the next version can read the last.
	const (
		cell      = "871f1d492ffffff"
		otherCell = "871f1d493ffffff"
		indexCell = "891f1d4926bffff"
		trip      = "0f2a6c1e-9d4b-4a77-8c31-6b1e5a2d9f80"
		at        = int64(1757512330000)
	)
	geo := map[string]wire.GeoEvent{
		"geo/driver_moved.json": {Tag: wire.TagDriverMoved, Cell: cell, AtMs: at, Moved: &wire.DriverMovedPayload{
			DriverID: "drv-000123", Epoch: 3, Seq: 41, Lat: 52.3702, Lng: 4.8952, Heading: 137.5,
			Status: wire.StatusIdle, IndexCell: indexCell, SentAtMs: at - 20,
		}},
		"geo/driver_entered_cell.json": {Tag: wire.TagDriverEnteredCell, Cell: cell, AtMs: at, Entered: &wire.DriverEnteredPayload{
			DriverID: "drv-000123", Epoch: 3, Seq: 42, Lat: 52.3711, Lng: 4.8963, Heading: 90,
			Status: wire.StatusIdle, IndexCell: indexCell, SentAtMs: at - 20, From: otherCell,
		}},
		"geo/driver_left_cell.json": {Tag: wire.TagDriverLeftCell, Cell: otherCell, AtMs: at, Left: &wire.DriverLeftPayload{
			DriverID: "drv-000123", Epoch: 3, Seq: 42, To: cell,
		}},
		"geo/match_requested.json": {Tag: wire.TagMatchRequested, Cell: cell, AtMs: at, Requested: &wire.MatchRequestPayload{
			TripID: trip, RiderID: "rider-000456", PickupLat: 52.3702, PickupLng: 4.8952,
			DropLat: 52.3584, DropLng: 4.8811, RequestedAtMs: at - 588, IdempotencyKey: trip + ":1",
		}},
		"geo/reserve_driver.json": {Tag: wire.TagReserveDriver, Cell: otherCell, AtMs: at, Reserve: &wire.ReservePayload{
			DriverID: "drv-000123", TripID: trip, ReplyCell: cell, DeadlineMs: at + 15000,
			RiderID: "rider-000456", PickupLat: 52.3702, PickupLng: 4.8952,
		}},
		"geo/reserve_result.json": {Tag: wire.TagReserveResult, Cell: cell, AtMs: at, Reserved: &wire.ReserveResultPayload{
			DriverID: "drv-000123", TripID: trip, OK: false, Reason: wire.RejectBusy,
		}},
		"geo/offer_replied.json": {Tag: wire.TagOfferReplied, Cell: cell, AtMs: at, Replied: &wire.OfferRepliedPayload{
			TripID: trip, DriverID: "drv-000123", Accepted: true,
		}},
	}
	for name, event := range geo {
		fixtures[name] = event
	}

	for name, value := range fixtures {
		raw, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "marshal %s: %v\n", name, err)
			os.Exit(1)
		}
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "mkdir %s: %v\n", filepath.Dir(path), err)
			os.Exit(1)
		}
		if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "write %s: %v\n", path, err)
			os.Exit(1)
		}
		fmt.Println(path)
	}
}
