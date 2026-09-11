package events_test

import (
	"testing"

	"github.com/ishakdeveloper/surge/services/gateway/internal/infrastructure/events"
)

// fakePusher holds who is connected here, by user and by role.
type fakePusher struct {
	connected map[string]bool
	roles     map[string]int

	pushed      []string
	broadcasted []string
}

func (f *fakePusher) Push(userID string, _ []byte) bool {
	f.pushed = append(f.pushed, userID)
	return f.connected[userID]
}

func (f *fakePusher) Broadcast(role string, _ []byte) int {
	f.broadcasted = append(f.broadcasted, role)
	return f.roles[role]
}

// A key is a person or, spelled role:<role>, everyone with that role. Support's
// queue doorbell must reach every connected ops user and never be taken for a
// user whose id happens to be "role:ops".
func TestDeliverRoutesByKey(t *testing.T) {
	doorbell := []byte(`{"_tag":"ChatChanged","chat":{"conversationId":"c1","kind":"support","tripId":"","lastSeq":1,"atMs":1}}`)

	cases := []struct {
		name          string
		key           string
		value         []byte
		want          events.Outcome
		wantPush      bool
		wantBroadcast bool
	}{
		{"a person connected here", "rider-1", doorbell, events.Delivered, true, false},
		{"a person connected elsewhere", "rider-2", doorbell, events.NotHere, true, false},
		{"support, connected here", "role:ops", doorbell, events.Delivered, false, true},
		{"a role nobody here holds", "role:driver", doorbell, events.NotHere, false, true},
		{"no key", "", doorbell, events.Malformed, false, false},
		{"a tag without its payload", "rider-1", []byte(`{"_tag":"ChatChanged"}`), events.Malformed, false, false},
		{"not JSON", "rider-1", []byte(`{`), events.Malformed, false, false},
	}
	for _, c := range cases {
		pusher := &fakePusher{connected: map[string]bool{"rider-1": true}, roles: map[string]int{"ops": 2}}

		got, _ := events.Deliver(pusher, []byte(c.key), c.value)
		if got != c.want {
			t.Errorf("%s: outcome %v, want %v", c.name, got, c.want)
		}
		if pushed := len(pusher.pushed) > 0; pushed != c.wantPush {
			t.Errorf("%s: pushed to %v", c.name, pusher.pushed)
		}
		if broadcasted := len(pusher.broadcasted) > 0; broadcasted != c.wantBroadcast {
			t.Errorf("%s: broadcast to %v", c.name, pusher.broadcasted)
		}
	}
}
