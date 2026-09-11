package wire

import "strings"

// Chat pushes on `ws.push`: doorbells, never messages.
//
// A message is stored by the chat service and read over REST. What travels
// here is only enough for a client to know it should read, and from where —
// which keeps the message shape in one place, the proto, and makes a push
// that arrives late, twice, out of order or never cost nothing: the client
// reads everything after the newest seq it holds, and a stale doorbell asks
// for nothing.
const (
	// TagChatChanged is a conversation with something new in it: a message, or
	// a change of status, assignee or closing time.
	TagChatChanged = "ChatChanged"
	// TagChatRead is another participant's read marker moving forward.
	TagChatRead = "ChatRead"
	// TagChatTyping is another participant typing. Shown for a few seconds and
	// then forgotten; it is never stored.
	TagChatTyping = "ChatTyping"
)

// ChatChange is the doorbell for a conversation.
type ChatChange struct {
	ConversationID string `json:"conversationId"`
	// Kind is "trip" or "support", so a client knows which screen the
	// conversation belongs on without reading it first.
	Kind string `json:"kind"`
	// TripID is the trip the conversation is about, and empty for none.
	TripID string `json:"tripId"`
	// LastSeq is the newest message's seq. A client already holding it has
	// nothing to read.
	LastSeq int64 `json:"lastSeq"`
	AtMs    int64 `json:"atMs"`
}

// ChatRead is where a participant has read up to.
type ChatRead struct {
	ConversationID string `json:"conversationId"`
	UserID         string `json:"userId"`
	Seq            int64  `json:"seq"`
	AtMs           int64  `json:"atMs"`
}

// ChatTyping is a participant typing, as of AtMs.
type ChatTyping struct {
	ConversationID string `json:"conversationId"`
	UserID         string `json:"userId"`
	AtMs           int64  `json:"atMs"`
}

// audiencePrefix marks a `ws.push` key naming a role rather than a person.
const audiencePrefix = "role:"

// RoleAudience is the `ws.push` key that reaches everyone connected with a
// role. Support's queue changing is news to whoever from support has it open,
// and the producer has no way to know who that is.
func RoleAudience(role string) string { return audiencePrefix + role }

// AudienceRole is the role a `ws.push` key names, if it names one.
func AudienceRole(key string) (string, bool) {
	role, found := strings.CutPrefix(key, audiencePrefix)
	return role, found && role != ""
}
