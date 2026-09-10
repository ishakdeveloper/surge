// Package repository implements domain.Repository.
//
// Two implementations, and the in-memory one is not a toy: it is what lets the
// service layer be tested exhaustively without a container, so the tests that
// cover the interesting logic run in milliseconds and on a machine with no
// Docker. The Postgres one is then only responsible for being a faithful
// translation of the same interface.
package repository

import (
	"context"
	"maps"
	"sort"
	"sync"

	"github.com/ishakdeveloper/surge/services/trip/internal/domain"
)

// InMemory is a repository backed by a map.
type InMemory struct {
	mu    sync.RWMutex
	trips map[string]domain.Trip
	// keys maps rider+idempotency key to a trip id, mirroring the unique index
	// the Postgres schema carries.
	keys map[string]string
}

func NewInMemory() *InMemory {
	return &InMemory{trips: make(map[string]domain.Trip), keys: make(map[string]string)}
}

func (r *InMemory) Create(_ context.Context, trip *domain.Trip) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.trips[trip.ID] = *trip
	if trip.IdempotencyKey != "" {
		r.keys[trip.RiderID+"\x00"+trip.IdempotencyKey] = trip.ID
	}
	return nil
}

func (r *InMemory) Get(_ context.Context, id string) (*domain.Trip, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	trip, ok := r.trips[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	// A copy, so a caller mutating the result cannot silently change stored
	// state — which is the behaviour Postgres has and the one the service is
	// written against.
	return &trip, nil
}

func (r *InMemory) FindByIdempotencyKey(_ context.Context, riderID, key string) (*domain.Trip, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	id, ok := r.keys[riderID+"\x00"+key]
	if !ok {
		return nil, domain.ErrNotFound
	}
	trip := r.trips[id]
	return &trip, nil
}

func (r *InMemory) Update(_ context.Context, trip *domain.Trip) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.trips[trip.ID]; !ok {
		return domain.ErrNotFound
	}
	r.trips[trip.ID] = *trip
	return nil
}

// Snapshot is for tests and the debug endpoint.
func (r *InMemory) Snapshot() map[string]domain.Trip {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return maps.Clone(r.trips)
}

func (r *InMemory) List(_ context.Context, filter domain.ListFilter) (domain.Page, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var found []domain.Trip
	for _, trip := range r.trips {
		if trip.RiderID != filter.RiderID {
			continue
		}
		if filter.Status != "" && trip.Status != filter.Status {
			continue
		}
		found = append(found, trip)
	}

	// Newest first, and the id breaks ties so the order is total. Without the
	// tie-break two trips created in the same millisecond could swap places
	// between requests, which is exactly what a cursor cannot survive.
	sort.Slice(found, func(a, b int) bool {
		if !found[a].CreatedAt.Equal(found[b].CreatedAt) {
			return found[a].CreatedAt.After(found[b].CreatedAt)
		}
		return found[a].ID > found[b].ID
	})

	if filter.Cursor != "" {
		for i, trip := range found {
			if trip.ID == filter.Cursor {
				found = found[i+1:]
				break
			}
		}
	}

	page := domain.Page{}
	if len(found) > filter.Limit {
		page.NextCursor = found[filter.Limit-1].ID
		found = found[:filter.Limit]
	}
	page.Trips = found

	return page, nil
}
