package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ishakdeveloper/surge/services/chat/internal/domain"
)

func TestCompose(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		quickReply string
		role       domain.Role
		kind       domain.Kind
		wantCode   string
		wantText   string
		invalid    bool
	}{
		{"text, trimmed", "  on the corner  ", "", domain.RoleRider, domain.KindTrip, "", "on the corner", false},
		{"a quick reply becomes its text", "", "driver.arrived", domain.RoleDriver, domain.KindTrip, "driver.arrived", "I'm here", false},
		// A rider cannot send the driver's "I'm here".
		{"someone else's quick reply", "", "driver.arrived", domain.RoleRider, domain.KindTrip, "", "", true},
		{"a quick reply for another kind", "", "support.looking_into_it", domain.RoleSupport, domain.KindTrip, "", "", true},
		{"both", "hi", "rider.coming_out", domain.RoleRider, domain.KindTrip, "", "", true},
		{"only spaces", "   ", "", domain.RoleRider, domain.KindTrip, "", "", true},
		{"at the limit", strings.Repeat("é", domain.MaxBody), "", domain.RoleRider, domain.KindTrip, "", strings.Repeat("é", domain.MaxBody), false},
		{"over the limit", strings.Repeat("a", domain.MaxBody+1), "", domain.RoleRider, domain.KindTrip, "", "", true},
		{"not UTF-8", "\xff\xfe", "", domain.RoleRider, domain.KindTrip, "", "", true},
	}
	for _, c := range cases {
		code, text, err := domain.Compose(c.body, c.quickReply, c.role, c.kind)
		var invalid *domain.InvalidError
		switch {
		case c.invalid && !errors.As(err, &invalid):
			t.Errorf("%s: want an InvalidError, got %v", c.name, err)
		case !c.invalid && err != nil:
			t.Errorf("%s: %v", c.name, err)
		case !c.invalid && (code != c.wantCode || text != c.wantText):
			t.Errorf("%s: got %q %q", c.name, code, text)
		}
	}
}

func TestEverySideHasQuickReplies(t *testing.T) {
	for _, side := range []struct {
		role domain.Role
		kind domain.Kind
	}{
		{domain.RoleDriver, domain.KindTrip},
		{domain.RoleRider, domain.KindTrip},
		{domain.RoleSupport, domain.KindSupport},
	} {
		if len(domain.QuickRepliesFor(side.role, side.kind)) == 0 {
			t.Errorf("%s in a %s conversation has no quick replies", side.role, side.kind)
		}
	}
	if replies := domain.QuickRepliesFor(domain.RoleRider, domain.KindSupport); len(replies) != 0 {
		t.Errorf("a rider asking support was offered %v", replies)
	}
}
