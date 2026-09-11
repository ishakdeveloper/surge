// Package domain is chat's rules: who is in a conversation, who may read and
// write in it, and when it stops taking messages. No transport, no storage.
package domain

import (
	"errors"
	"time"
)

// Kind is who a conversation is between.
type Kind string

const (
	// KindTrip is the rider and the driver of one trip.
	KindTrip Kind = "trip"
	// KindSupport is a rider or a driver, and whoever from support claims it.
	KindSupport Kind = "support"
)

// Status is whether a conversation was ended by somebody. A trip conversation
// is never resolved; it closes by the clock instead, at ClosesAt.
type Status string

const (
	StatusOpen     Status = "open"
	StatusResolved Status = "resolved"
)

// Role is the side of a conversation someone is on, which is what a message
// is labelled with.
type Role string

const (
	RoleRider   Role = "rider"
	RoleDriver  Role = "driver"
	RoleSupport Role = "support"
)

var (
	ErrNotFound = errors.New("chat: not found")
	// ErrClosed is a conversation that no longer takes messages: past its
	// trip's grace window, or resolved.
	ErrClosed = errors.New("chat: conversation is closed")
	// ErrNotAllowed is a request the caller's role cannot make here, such as
	// support writing in a rider's conversation with their driver.
	ErrNotAllowed = errors.New("chat: not allowed")
	// ErrClaimed is a support conversation somebody else from support holds.
	ErrClaimed = errors.New("chat: claimed by someone else")
)

// Participant is someone in a conversation, and how far they have read.
type Participant struct {
	UserID      string
	Role        Role
	LastReadSeq int64
}

type Conversation struct {
	ID     string
	Kind   Kind
	TripID string
	Status Status
	// Subject is what a support conversation is about, in the requester's
	// words. Empty for a trip conversation.
	Subject string
	// RequesterID is the rider of a trip conversation, and whoever opened a
	// support one.
	RequesterID string
	// AssigneeID is who from support is handling a support conversation. Empty
	// until somebody claims it.
	AssigneeID string
	// LastSeq is the seq of the newest message; messages are numbered from 1
	// with no gaps.
	LastSeq int64
	// ClosesAt is when sending stops, and zero while nothing has ended it.
	ClosesAt  time.Time
	CreatedAt time.Time
	UpdatedAt time.Time

	Participants []Participant
}

// NewTripConversation is the conversation a trip opens once a driver accepts
// it: its rider and that driver, nobody else.
func NewTripConversation(id, tripID, riderID, driverID string, now time.Time) *Conversation {
	return &Conversation{
		ID: id, Kind: KindTrip, TripID: tripID, Status: StatusOpen,
		RequesterID: riderID, CreatedAt: now, UpdatedAt: now,
		Participants: []Participant{
			{UserID: riderID, Role: RoleRider},
			{UserID: driverID, Role: RoleDriver},
		},
	}
}

// NewSupportConversation is a rider or driver asking support for help,
// optionally about a trip they were on.
func NewSupportConversation(id, requesterID string, role Role, tripID, subject string, now time.Time) *Conversation {
	return &Conversation{
		ID: id, Kind: KindSupport, TripID: tripID, Status: StatusOpen,
		Subject: subject, RequesterID: requesterID, CreatedAt: now, UpdatedAt: now,
		Participants: []Participant{{UserID: requesterID, Role: role}},
	}
}

// ClosesAfter is when a trip conversation stops taking messages, given when
// its trip ended: long enough after the drop-off for "I left my bag in your
// car", and no longer.
func ClosesAfter(ended time.Time, grace time.Duration) time.Time {
	return ended.Add(grace)
}

// OpenAt is whether the conversation takes messages at now.
func (c *Conversation) OpenAt(now time.Time) bool {
	if c.Status == StatusResolved {
		return false
	}
	return c.ClosesAt.IsZero() || now.Before(c.ClosesAt)
}

// Participant returns the participant with this user id, if they are one.
func (c *Conversation) Participant(userID string) (Participant, bool) {
	for _, participant := range c.Participants {
		if participant.UserID == userID {
			return participant, true
		}
	}
	return Participant{}, false
}

// Others is everyone in the conversation but userID: who hears about what
// they did.
func (c *Conversation) Others(userID string) []string {
	others := make([]string, 0, len(c.Participants))
	for _, participant := range c.Participants {
		if participant.UserID != userID {
			others = append(others, participant.UserID)
		}
	}
	return others
}

// Viewer is who is asking.
type Viewer struct {
	UserID string
	// Support is the ops role: it reads every conversation, and writes only in
	// support ones.
	Support bool
}

// CanRead is the read rule: anyone in the conversation, or support.
//
// Support reads trip conversations for the same reason it can read the trip:
// a rider who reports a driver is asking somebody to look at what was said.
func (c *Conversation) CanRead(viewer Viewer) bool {
	_, in := c.Participant(viewer.UserID)
	return in || viewer.Support
}

// CheckPost is the write rule. It returns claim=true when the viewer is
// support writing in an unclaimed support conversation, which claims it:
// answering is taking it on.
//
// ErrNotFound for someone who may not read it at all, so a writer cannot learn
// that a conversation exists by trying to post in it.
func (c *Conversation) CheckPost(viewer Viewer, now time.Time) (claim bool, err error) {
	if !c.CanRead(viewer) {
		return false, ErrNotFound
	}
	if !c.OpenAt(now) {
		return false, ErrClosed
	}
	_, in := c.Participant(viewer.UserID)
	switch {
	case in:
		return false, nil
	case c.Kind == KindTrip:
		// Support reads a rider's conversation with their driver; it does not
		// join it.
		return false, ErrNotAllowed
	case c.AssigneeID == "":
		return true, nil
	default:
		return false, ErrClaimed
	}
}
