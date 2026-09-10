package wire_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ishakdeveloper/surge/shared/wire"
)

// The gateway's WebSocket protocol is the one contract in this system that is
// not generated from anything. The REST surface comes from `proto/trip.proto`
// and `packages/domain/test/api/ApiContract.test.ts` catches drift against the
// generated OpenAPI; the Kafka topics are Go on both ends. This transport has
// Go on one side and TypeScript on the other, hand-written, which is exactly
// the arrangement that produced the drift in the starter this project borrows
// from — its `contracts.ts` and its `amqp.go` disagree about which trip events
// exist and neither side notices.
//
// So the fixtures in testdata/ are the contract. This file holds Go to them;
// `packages/domain/test/realtime/Wire.test.ts` holds TypeScript to the same
// files. Rename a field on either side and one of the two suites fails, which
// is the only reason to prefer a shared file over two sets of assertions.
//
// Regenerate the server-direction fixtures with `make wire-fixtures`. The
// client-direction ones are what the browser emits and are edited by hand;
// changing one without changing `packages/domain/src/realtime/Wire.ts` is
// caught by the TypeScript half.

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return raw
}

// assertMarshalsTo compares parsed JSON rather than bytes, so indentation and
// key order are not part of the contract. The field names and the values are.
func assertMarshalsTo(t *testing.T, value any, name string) {
	t.Helper()

	produced, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got, want any
	if err := json.Unmarshal(produced, &got); err != nil {
		t.Fatalf("reparse produced: %v", err)
	}
	if err := json.Unmarshal(fixture(t, name), &want); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	gotCanonical, _ := json.Marshal(got)
	wantCanonical, _ := json.Marshal(want)
	if string(gotCanonical) != string(wantCanonical) {
		t.Errorf("%s drifted\n got: %s\nwant: %s", name, gotCanonical, wantCanonical)
	}
}

func TestServerMessagesMatchFixtures(t *testing.T) {
	assertMarshalsTo(t, wire.ServerMessage{Tag: wire.TagServerWelcome}, "server_welcome.json")

	assertMarshalsTo(t, wire.ServerMessage{Tag: wire.TagServerError, Error: "token expired"}, "server_error.json")

	assertMarshalsTo(t, wire.ServerMessage{Tag: wire.TagTripUpdated, Trip: &wire.TripUpdate{
		TripID:   "0f2a6c1e-9d4b-4a77-8c31-6b1e5a2d9f80",
		RiderID:  "rider-000456",
		DriverID: "drv-000123",
		Status:   "TRIP_STATUS_ACCEPTED",
		AtMs:     1757512331000,
	}}, "server_trip_updated.json")

	assertMarshalsTo(t, wire.ServerMessage{Tag: wire.TagOffer, Offer: &wire.Offer{
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
	}}, "server_offer.json")
}

func TestClientMessagesDecodeFromFixtures(t *testing.T) {
	var heartbeat wire.ClientMessage
	if err := json.Unmarshal(fixture(t, "client_heartbeat.json"), &heartbeat); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if heartbeat.Tag != wire.TagClientHeartbeat {
		t.Errorf("heartbeat tag = %q", heartbeat.Tag)
	}

	var ping wire.ClientMessage
	if err := json.Unmarshal(fixture(t, "client_ping.json"), &ping); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if ping.Tag != wire.TagClientPing {
		t.Fatalf("ping tag = %q", ping.Tag)
	}
	if ping.Ping == nil {
		t.Fatal("ping payload is absent, which the hub reads as a malformed frame")
	}
	if ping.Ping.Epoch != 1757512300000 || ping.Ping.Seq != 42 {
		t.Errorf("ordering fields = (%d, %d)", ping.Ping.Epoch, ping.Ping.Seq)
	}
	if ping.Ping.Lat != 52.3702 || ping.Ping.Lng != 4.8952 {
		t.Errorf("position = (%f, %f)", ping.Ping.Lat, ping.Ping.Lng)
	}
	if ping.Ping.Status != wire.StatusIdle {
		t.Errorf("status = %q", ping.Ping.Status)
	}
	if ping.Ping.SpeedMps != 8.3 || ping.Ping.Heading != 137.5 {
		t.Errorf("motion = (%f, %f)", ping.Ping.SpeedMps, ping.Ping.Heading)
	}
	if ping.Ping.SentAtMs != 1757512329412 {
		t.Errorf("sentAtMs = %d", ping.Ping.SentAtMs)
	}

	// The client does not send a driver id, and must not be able to: the hub
	// overwrites it from the verified token, because a client that could name
	// its own driver id could report positions for somebody else and be
	// dispatched their rides. An empty value here is the schema in
	// packages/domain omitting the field, and the ping is invalid until the
	// gateway fills it in — which is the property worth pinning.
	if ping.Ping.DriverID != "" {
		t.Errorf("client named a driver id: %q", ping.Ping.DriverID)
	}
	if ping.Ping.Valid() {
		t.Error("a ping without an identity should not validate before the gateway fills one in")
	}
	ping.Ping.DriverID = "drv-000123"
	if !ping.Ping.Valid() {
		t.Error("the same ping should validate once the gateway has")
	}

	var reply wire.ClientMessage
	if err := json.Unmarshal(fixture(t, "client_offer_reply.json"), &reply); err != nil {
		t.Fatalf("reply: %v", err)
	}
	if reply.Tag != wire.TagClientOfferReply {
		t.Fatalf("reply tag = %q", reply.Tag)
	}
	if reply.Reply == nil {
		t.Fatal("reply payload is absent")
	}
	if reply.Reply.TripID != "0f2a6c1e-9d4b-4a77-8c31-6b1e5a2d9f80" || !reply.Reply.Accepted {
		t.Errorf("reply = %+v", *reply.Reply)
	}
	// Without this the hub drops the frame: only the shard holding the
	// reservation can resolve an offer, and this is how it is addressed.
	if reply.ReplyCell != "871f1d492ffffff" {
		t.Errorf("replyCell = %q", reply.ReplyCell)
	}
}

// The gateway forwards ws.push records verbatim, so a tag without its payload
// must be caught before it reaches a client whose decoder would reject it.
func TestServerMessageValid(t *testing.T) {
	cases := []struct {
		name    string
		message wire.ServerMessage
		want    bool
	}{
		{"offer", wire.ServerMessage{Tag: wire.TagOffer, Offer: &wire.Offer{}}, true},
		{"offer without payload", wire.ServerMessage{Tag: wire.TagOffer}, false},
		{"trip", wire.ServerMessage{Tag: wire.TagTripUpdated, Trip: &wire.TripUpdate{TripID: "t"}}, true},
		{"trip without id", wire.ServerMessage{Tag: wire.TagTripUpdated, Trip: &wire.TripUpdate{}}, false},
		{"trip without payload", wire.ServerMessage{Tag: wire.TagTripUpdated}, false},
		{"welcome", wire.ServerMessage{Tag: wire.TagServerWelcome}, true},
		{"unknown", wire.ServerMessage{Tag: "SurgeUpdated"}, false},
	}
	for _, c := range cases {
		if got := c.message.Valid(); got != c.want {
			t.Errorf("%s: Valid() = %v, want %v", c.name, got, c.want)
		}
	}
}
