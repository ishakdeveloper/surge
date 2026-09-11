package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/chat/internal/domain"
)

var now = time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)

// Who may write where, written down as a table so a change to the rule is a
// visible diff rather than a quiet edit to a switch.
func TestWhoMayWrite(t *testing.T) {
	trip := domain.NewTripConversation("c-trip", "trip-1", "rider-1", "drv-1", now)

	ended := domain.NewTripConversation("c-ended", "trip-2", "rider-1", "drv-1", now)
	ended.ClosesAt = now

	unclaimed := domain.NewSupportConversation("c-help", "rider-1", domain.RoleRider, "", "lost bag", now)

	claimed := domain.NewSupportConversation("c-held", "rider-1", domain.RoleRider, "", "", now)
	claimed.AssigneeID = "ops-1"
	claimed.Participants = append(claimed.Participants, domain.Participant{UserID: "ops-1", Role: domain.RoleSupport})

	resolved := domain.NewSupportConversation("c-done", "rider-1", domain.RoleRider, "", "", now)
	resolved.Status = domain.StatusResolved

	rider := domain.Viewer{UserID: "rider-1"}
	driver := domain.Viewer{UserID: "drv-1"}
	stranger := domain.Viewer{UserID: "rider-2"}
	ops := domain.Viewer{UserID: "ops-1", Support: true}
	otherOps := domain.Viewer{UserID: "ops-2", Support: true}

	cases := []struct {
		name         string
		conversation *domain.Conversation
		viewer       domain.Viewer
		claim        bool
		err          error
	}{
		{"the rider, in their trip", trip, rider, false, nil},
		{"the driver, in their trip", trip, driver, false, nil},
		// Not "forbidden": a stranger learns nothing, not even that it exists.
		{"a stranger, in someone's trip", trip, stranger, false, domain.ErrNotFound},
		{"support, in a trip", trip, ops, false, domain.ErrNotAllowed},
		{"the rider, after the grace window", ended, rider, false, domain.ErrClosed},
		{"the requester, in their support chat", unclaimed, rider, false, nil},
		{"support, answering an unclaimed one", unclaimed, ops, true, nil},
		{"a stranger, in someone's support chat", unclaimed, stranger, false, domain.ErrNotFound},
		{"the assignee", claimed, ops, false, nil},
		{"support, in one somebody else holds", claimed, otherOps, false, domain.ErrClaimed},
		{"the requester, once resolved", resolved, rider, false, domain.ErrClosed},
	}
	for _, c := range cases {
		claim, err := c.conversation.CheckPost(c.viewer, now)
		if !errors.Is(err, c.err) || claim != c.claim {
			t.Errorf("%s: claim=%v err=%v, want claim=%v err=%v", c.name, claim, err, c.claim, c.err)
		}
	}
}

// Support reads every conversation, which is what lets it look into a
// report; nobody else reads one they are not in.
func TestWhoMayRead(t *testing.T) {
	trip := domain.NewTripConversation("c", "trip-1", "rider-1", "drv-1", now)
	for viewer, want := range map[domain.Viewer]bool{
		{UserID: "rider-1"}:              true,
		{UserID: "drv-1"}:                true,
		{UserID: "rider-2"}:              false,
		{UserID: "ops-1", Support: true}: true,
	} {
		if got := trip.CanRead(viewer); got != want {
			t.Errorf("%+v: CanRead %v, want %v", viewer, got, want)
		}
	}
}

func TestStateFollowsTheClock(t *testing.T) {
	conversation := domain.NewTripConversation("c", "trip-1", "rider-1", "drv-1", now)
	if got := conversation.StateAt(now); got != domain.StateOpen {
		t.Errorf("a running trip's conversation is %s", got)
	}

	conversation.ClosesAt = domain.ClosesAfter(now, time.Hour)
	if got := conversation.StateAt(now.Add(59 * time.Minute)); got != domain.StateOpen {
		t.Errorf("inside the grace window: %s", got)
	}
	// Closed at the instant, not a moment after: the send and the close
	// cannot both win.
	if got := conversation.StateAt(now.Add(time.Hour)); got != domain.StateClosed {
		t.Errorf("at the close: %s", got)
	}

	support := domain.NewSupportConversation("s", "rider-1", domain.RoleRider, "", "", now)
	support.Status = domain.StatusResolved
	if got := support.StateAt(now); got != domain.StateResolved {
		t.Errorf("resolved support is %s", got)
	}
}

func TestOthersAreWhoHearsAboutIt(t *testing.T) {
	conversation := domain.NewTripConversation("c", "trip-1", "rider-1", "drv-1", now)
	if others := conversation.Others("rider-1"); len(others) != 1 || others[0] != "drv-1" {
		t.Errorf("the rider's message goes to %v", others)
	}
	if everyone := conversation.Others(""); len(everyone) != 2 {
		t.Errorf("a change nobody made goes to %v", everyone)
	}
}
