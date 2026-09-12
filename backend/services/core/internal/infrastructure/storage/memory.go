package storage

import (
	"context"
	"encoding/base64"
	"sync"
	"time"
)

// Memory keeps photos in the process, for a laptop without R2's keys and for
// tests. Gone when the process stops.
//
// A link is the photo itself, as a data URL, so the web app shows it with no
// object store and no route to serve it from.
type Memory struct {
	mu      sync.Mutex
	objects map[string]object
}

type object struct {
	body        []byte
	contentType string
}

func NewMemory() *Memory { return &Memory{objects: map[string]object{}} }

func (m *Memory) Put(_ context.Context, key string, body []byte, contentType string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = object{body: append([]byte(nil), body...), contentType: contentType}
	return nil
}

func (m *Memory) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

func (m *Memory) URL(key string, _ time.Time) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	stored, ok := m.objects[key]
	if !ok {
		// Recorded by an earlier run of this process; the photo went with it.
		return "", nil
	}
	return "data:" + stored.contentType + ";base64," + base64.StdEncoding.EncodeToString(stored.body), nil
}

// Has reports whether a photo is stored, for tests.
func (m *Memory) Has(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.objects[key]
	return ok
}

// Len is how many photos are stored, for tests.
func (m *Memory) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.objects)
}
