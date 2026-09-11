package domain

import "time"

// State is what a conversation is to someone looking at it now: taking
// messages, closed by its trip's end, or resolved by somebody.
type State string

const (
	StateOpen     State = "open"
	StateClosed   State = "closed"
	StateResolved State = "resolved"
)

// StateAt is the conversation's state at now.
func (c *Conversation) StateAt(now time.Time) State {
	switch {
	case c.Status == StatusResolved:
		return StateResolved
	case c.OpenAt(now):
		return StateOpen
	default:
		return StateClosed
	}
}

// ListFilter narrows a list of conversations.
type ListFilter struct {
	// UserID lists the conversations this person is in. Empty lists everyone's,
	// which only support's queue does.
	UserID string
	// Kind and State are optional.
	Kind  Kind
	State State
	// Now is what State is judged against.
	Now    time.Time
	Limit  int
	Cursor string
}

// Page is one page of conversations, newest first.
type Page struct {
	Conversations []Conversation
	// NextCursor is empty on the last page.
	NextCursor string
}

// MessageQuery is a page of a conversation. After reads forward from a seq,
// Before reads back from one, and with neither it is the newest page.
type MessageQuery struct {
	After  int64
	Before int64
	Limit  int
}

// ConversationPage and MessagePage clamp a requested page size.
func ConversationPage(requested int) int { return clamp(requested, 20, 100) }
func MessagePage(requested int) int      { return clamp(requested, 50, 200) }

func clamp(requested, fallback, most int) int {
	switch {
	case requested <= 0:
		return fallback
	case requested > most:
		return most
	default:
		return requested
	}
}
