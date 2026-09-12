package storage

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ishakdeveloper/surge/services/fleet/internal/service"
)

// Memory is the store a laptop and the tests run on.
//
// It hands out links to a loopback address that nothing serves, because the
// point of the upload path is that the file does not come through this API —
// a fake that accepted bytes here would be testing the wrong shape. What it
// does hold is which keys were asked for, which is what a test asserts on.
type Memory struct {
	mu      sync.Mutex
	uploads map[string]service.Upload
}

func NewMemory() *Memory {
	return &Memory{uploads: make(map[string]service.Upload)}
}

func (m *Memory) Upload(_ context.Context, key, _ string, _ int64, now time.Time) (service.Upload, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	upload := service.Upload{
		URL:       fmt.Sprintf("http://127.0.0.1:0/uploads/%s", key),
		ExpiresAt: now.Add(uploadWindow),
	}
	m.uploads[key] = upload
	return upload, nil
}

func (m *Memory) View(key string, _ time.Time) (string, error) {
	return fmt.Sprintf("http://127.0.0.1:0/documents/%s", key), nil
}

// Asked reports whether a link was handed out for this key, for tests.
func (m *Memory) Asked(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.uploads[key]
	return ok
}
