package wire_test

import (
	"testing"

	"github.com/ishakdeveloper/surge/shared/wire"
)

// The three chat doorbells both languages decode: Go emits them, and
// packages/domain/test/realtime/Wire.test.ts holds TypeScript to the same
// files.
func TestChatPushesMatchTheFixtures(t *testing.T) {
	const conversation = "5b1d7e2a-3c4f-4e8a-9b6d-2f7c1a0e8d43"
	cases := []struct {
		fixture string
		message wire.ServerMessage
	}{
		{"server_chat_changed.json", wire.ServerMessage{Tag: wire.TagChatChanged, Chat: &wire.ChatChange{
			ConversationID: conversation, Kind: "trip",
			TripID: "0f2a6c1e-9d4b-4a77-8c31-6b1e5a2d9f80", LastSeq: 7, AtMs: 1757512350000,
		}}},
		{"server_chat_read.json", wire.ServerMessage{Tag: wire.TagChatRead, ChatRead: &wire.ChatRead{
			ConversationID: conversation, UserID: "drv-000123", Seq: 7, AtMs: 1757512352000,
		}}},
		{"server_chat_typing.json", wire.ServerMessage{Tag: wire.TagChatTyping, ChatTyping: &wire.ChatTyping{
			ConversationID: conversation, UserID: "rider-000456", AtMs: 1757512351000,
		}}},
	}
	for _, c := range cases {
		if !c.message.Valid() {
			t.Fatalf("%s: not valid, so the gateway would drop it", c.fixture)
		}
		assertMarshalsTo(t, c.message, c.fixture)
	}
}

// A doorbell naming no conversation, or a receipt naming nobody, is one no
// client could act on, and the gateway drops it rather than deliver it.
func TestChatPushesWithoutTheirSubjectAreRefused(t *testing.T) {
	for name, message := range map[string]wire.ServerMessage{
		"changed, no payload":       {Tag: wire.TagChatChanged},
		"changed, no conversation":  {Tag: wire.TagChatChanged, Chat: &wire.ChatChange{LastSeq: 1}},
		"read, no reader":           {Tag: wire.TagChatRead, ChatRead: &wire.ChatRead{ConversationID: "c"}},
		"typing, no conversation":   {Tag: wire.TagChatTyping, ChatTyping: &wire.ChatTyping{UserID: "u"}},
		"typing, no payload at all": {Tag: wire.TagChatTyping},
	} {
		if message.Valid() {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestRoleAudience(t *testing.T) {
	if role, ok := wire.AudienceRole(wire.RoleAudience("ops")); !ok || role != "ops" {
		t.Errorf("round trip gave %q, %v", role, ok)
	}
	for _, key := range []string{"rider-000456", "role:", ""} {
		if _, ok := wire.AudienceRole(key); ok {
			t.Errorf("%q read as an audience", key)
		}
	}
}
