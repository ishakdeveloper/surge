package wire_test

import (
	"testing"

	"github.com/ishakdeveloper/surge/shared/wire"
)

// The push both languages decode: Go emits it, and
// packages/domain/test/realtime/Wire.test.ts holds TypeScript to the same file.
func TestPaymentsChangedMatchesTheFixture(t *testing.T) {
	message := wire.ServerMessage{Tag: wire.TagPaymentsChanged, Payments: &wire.PaymentsChange{
		TripID: "0f2a6c1e-9d4b-4a77-8c31-6b1e5a2d9f80",
		AtMs:   1757512345000,
	}}
	if !message.Valid() {
		t.Fatal("a payments push is not valid, so the gateway would drop it")
	}
	assertMarshalsTo(t, message, "server_payments_changed.json")

	// A change about no one trip — a card saved — is still a push worth sending.
	untied := wire.ServerMessage{Tag: wire.TagPaymentsChanged, Payments: &wire.PaymentsChange{AtMs: 1}}
	if !untied.Valid() {
		t.Error("a payments push without a trip was refused")
	}
	if (wire.ServerMessage{Tag: wire.TagPaymentsChanged}).Valid() {
		t.Error("a payments push with no payload was accepted")
	}
}
