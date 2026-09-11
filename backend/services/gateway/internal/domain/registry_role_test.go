package domain_test

import (
	"testing"

	"github.com/ishakdeveloper/surge/services/gateway/internal/domain"
)

// Support's queue doorbell goes to everyone connected as ops, and only them;
// a user who reconnects is counted once, on their new socket.
func TestWithRoleFindsEveryoneWithTheRole(t *testing.T) {
	registry := domain.NewRegistry()
	registry.Add(domain.NewConnection("c1", "ops-1", "ops", now))
	registry.Add(domain.NewConnection("c2", "ops-2", "ops", now))
	registry.Add(domain.NewConnection("c3", "rider-1", "rider", now))
	replacement := domain.NewConnection("c4", "ops-1", "ops", now)
	registry.Add(replacement)

	ops := registry.WithRole("ops")
	if len(ops) != 2 {
		t.Fatalf("%d ops connections, want 2", len(ops))
	}
	for _, connection := range ops {
		if connection.UserID == "ops-1" && connection != replacement {
			t.Error("the replaced socket is still listed")
		}
	}
	if drivers := registry.WithRole("driver"); len(drivers) != 0 {
		t.Errorf("%d drivers in a registry with none", len(drivers))
	}
}
