// Package repository keeps profiles: in Postgres, and in memory for the
// service's tests.
package repository

import (
	"context"
	"sync"
	"time"

	"github.com/ishakdeveloper/surge/services/core/internal/domain"
)

type Memory struct {
	mu       sync.Mutex
	profiles map[string]domain.Profile
}

func NewMemory() *Memory { return &Memory{profiles: map[string]domain.Profile{}} }

func (m *Memory) Get(_ context.Context, userID string) (domain.Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if profile, ok := m.profiles[userID]; ok {
		return profile, nil
	}
	return domain.Profile{UserID: userID}, nil
}

func (m *Memory) SaveName(_ context.Context, userID, name string, at time.Time) (domain.Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	profile := m.profiles[userID]
	profile.UserID, profile.DisplayName, profile.UpdatedAt = userID, name, at
	m.profiles[userID] = profile
	return profile, nil
}

func (m *Memory) SaveAvatar(_ context.Context, userID, key string, at time.Time) (domain.Profile, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	profile := m.profiles[userID]
	previous := profile.AvatarKey
	profile.UserID, profile.AvatarKey, profile.UpdatedAt = userID, key, at
	m.profiles[userID] = profile
	return profile, previous, nil
}
