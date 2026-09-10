package repository

import (
	"context"
	"sync"
	"time"

	"github.com/ishakdeveloper/surge/trip/internal/domain"
	"github.com/ishakdeveloper/surge/trip/internal/service"
)

// FareCache holds quotes between preview and booking.
//
// In memory on purpose, and it is worth being explicit about the trade. Most
// quotes are never accepted — a rider dragging a pin generates three per
// movement — they expire in minutes, and writing them to Postgres would make
// browsing a map a write workload.
//
// The cost is that a fare is bound to the process that quoted it, so a rider
// who previews against one instance and books against another is told the fare
// has gone. That is acceptable while trip runs as one process and is the reason
// this is behind an interface: moving it to Redis is a new implementation, not
// a change to the service.
type FareCache struct {
	mu    sync.Mutex
	fares map[string]domain.Fare
	now   func() time.Time
}

func NewFareCache() *FareCache {
	return &FareCache{fares: make(map[string]domain.Fare), now: time.Now}
}

func (c *FareCache) Put(_ context.Context, fare domain.Fare) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.fares[fare.ID] = fare
	return nil
}

func (c *FareCache) Get(_ context.Context, id string) (domain.Fare, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	fare, ok := c.fares[id]
	if !ok {
		return domain.Fare{}, service.ErrFareNotFound
	}
	return fare, nil
}

// Sweep drops expired quotes. Without it a busy day's abandoned previews are a
// slow leak — the map only ever grows, and nothing else would ever remove them.
func (c *FareCache) Sweep() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	removed := 0
	for id, fare := range c.fares {
		// A grace period past expiry so a fare rejected as expired can still be
		// read back to say so, rather than becoming "not found".
		if now.After(fare.ExpiresAt.Add(time.Minute)) {
			delete(c.fares, id)
			removed++
		}
	}
	return removed
}

func (c *FareCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.fares)
}
