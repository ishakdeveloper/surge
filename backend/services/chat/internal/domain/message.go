package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// MaxBody is the longest message, in characters. A chat about a pickup is
	// a sentence or two; this is room for a paragraph to support.
	MaxBody = 1000
	// MaxSubject is the longest support subject, in characters.
	MaxSubject = 120
)

// Message is one message, numbered within its conversation.
type Message struct {
	ID             string
	ConversationID string
	Seq            int64
	SenderID       string
	SenderRole     Role
	// QuickReply is the catalogue code when this was a quick reply, and empty
	// for text.
	QuickReply string
	Body       string
	// ClientMessageID is the key the sending client made up, so a send retried
	// after a lost response is found rather than stored twice.
	ClientMessageID string
	CreatedAt       time.Time
}

// InvalidError is a message that cannot be sent as written. Its reason is for
// the person who wrote it.
type InvalidError struct{ Reason string }

func (e *InvalidError) Error() string { return "chat: " + e.Reason }

// Compose turns what somebody typed or tapped into what is stored: the quick
// reply's code (empty for text) and the body.
func Compose(body, quickReply string, role Role, kind Kind) (code, text string, err error) {
	body = strings.TrimSpace(body)
	switch {
	case body != "" && quickReply != "":
		return "", "", &InvalidError{Reason: "send text or a quick reply, not both"}
	case quickReply != "":
		reply, ok := LookupQuickReply(quickReply, role, kind)
		if !ok {
			return "", "", &InvalidError{Reason: "that quick reply is not available here"}
		}
		return reply.Code, reply.Text, nil
	case body == "":
		return "", "", &InvalidError{Reason: "a message cannot be empty"}
	case !utf8.ValidString(body):
		return "", "", &InvalidError{Reason: "a message must be valid UTF-8"}
	case utf8.RuneCountInString(body) > MaxBody:
		return "", "", &InvalidError{Reason: fmt.Sprintf("a message is at most %d characters", MaxBody)}
	}
	return "", body, nil
}

// Subject tidies a support subject, which may be empty.
func Subject(subject string) (string, error) {
	subject = strings.TrimSpace(subject)
	if !utf8.ValidString(subject) || utf8.RuneCountInString(subject) > MaxSubject {
		return "", &InvalidError{Reason: fmt.Sprintf("a subject is at most %d characters", MaxSubject)}
	}
	return subject, nil
}
