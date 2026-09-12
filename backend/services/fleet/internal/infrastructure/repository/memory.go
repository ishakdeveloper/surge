package repository

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/ishakdeveloper/surge/services/fleet/internal/domain"
	"github.com/ishakdeveloper/surge/services/fleet/internal/service"
)

// Memory is the repository the service's tests run on.
//
// It mirrors the rules the Postgres one enforces in SQL — one live vehicle per
// plate, a queue ordered by the oldest document still waiting, expiry by date
// — so a test that passes here is about the service rather than about a map.
type Memory struct {
	mu        sync.Mutex
	drivers   map[string]domain.Driver
	vehicles  map[string]domain.Vehicle
	documents map[string]domain.Document
	facts     []service.Fact
}

func NewMemory() *Memory {
	return &Memory{
		drivers:   make(map[string]domain.Driver),
		vehicles:  make(map[string]domain.Vehicle),
		documents: make(map[string]domain.Document),
	}
}

func (m *Memory) EnsureDriver(_ context.Context, driverID string, now time.Time) (domain.Driver, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	driver, ok := m.drivers[driverID]
	if !ok {
		driver = domain.Driver{
			ID: driverID, Status: domain.StatusOnboarding, Identity: domain.IdentityUnstarted,
			CreatedAt: now, UpdatedAt: now,
		}
		m.drivers[driverID] = driver
	}
	return driver, nil
}

func (m *Memory) Driver(_ context.Context, driverID string) (domain.Driver, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	driver, ok := m.drivers[driverID]
	if !ok {
		return domain.Driver{}, fmt.Errorf("repository: driver: %w", domain.ErrNotFound)
	}
	return driver, nil
}

func (m *Memory) DriverBySession(_ context.Context, sessionID string) (domain.Driver, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, driver := range m.drivers {
		if driver.IdentitySessionID == sessionID && sessionID != "" {
			return driver, nil
		}
	}
	return domain.Driver{}, fmt.Errorf("repository: driver by session: %w", domain.ErrNotFound)
}

func (m *Memory) SaveDriver(_ context.Context, driver domain.Driver, fact *service.Fact) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.drivers[driver.ID]; !ok {
		return fmt.Errorf("repository: save driver: %w", domain.ErrNotFound)
	}
	m.drivers[driver.ID] = driver
	if fact != nil {
		m.facts = append(m.facts, *fact)
	}
	return nil
}

// Facts is the news this repository was asked to publish, for tests.
func (m *Memory) Facts() []service.Fact {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.facts)
}

func (m *Memory) Papers(_ context.Context, driverID string) (domain.Papers, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	driver, ok := m.drivers[driverID]
	if !ok {
		return domain.Papers{}, fmt.Errorf("repository: papers: %w", domain.ErrNotFound)
	}

	papers := domain.Papers{Driver: driver}
	for _, vehicle := range m.vehicles {
		if vehicle.DriverID == driverID {
			papers.Vehicles = append(papers.Vehicles, vehicle)
		}
	}
	for _, document := range m.documents {
		if document.DriverID == driverID {
			papers.Documents = append(papers.Documents, document)
		}
	}
	sort.Slice(papers.Vehicles, func(i, j int) bool {
		return papers.Vehicles[i].CreatedAt.After(papers.Vehicles[j].CreatedAt)
	})
	sort.Slice(papers.Documents, func(i, j int) bool {
		return papers.Documents[i].CreatedAt.Before(papers.Documents[j].CreatedAt)
	})
	return papers, nil
}

func (m *Memory) Vehicle(_ context.Context, vehicleID string) (domain.Vehicle, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	vehicle, ok := m.vehicles[vehicleID]
	if !ok {
		return domain.Vehicle{}, fmt.Errorf("repository: vehicle: %w", domain.ErrNotFound)
	}
	return vehicle, nil
}

func (m *Memory) SaveVehicle(_ context.Context, vehicle domain.Vehicle) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, existing := range m.vehicles {
		if existing.ID == vehicle.ID || existing.Plate != vehicle.Plate {
			continue
		}
		if existing.Status != domain.VehicleRetired && vehicle.Status != domain.VehicleRetired {
			return domain.ErrPlateTaken
		}
	}
	m.vehicles[vehicle.ID] = vehicle
	return nil
}

func (m *Memory) Document(_ context.Context, documentID string) (domain.Document, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	document, ok := m.documents[documentID]
	if !ok {
		return domain.Document{}, fmt.Errorf("repository: document: %w", domain.ErrNotFound)
	}
	return document, nil
}

func (m *Memory) SaveDocument(_ context.Context, document domain.Document) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.documents[document.ID] = document
	return nil
}

func (m *Memory) Queue(_ context.Context, limit int, cursor string) (service.QueuePage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	limit = QueueLimit(limit)
	waiting := make(map[string]service.QueueItem)
	for _, document := range m.documents {
		if document.Status != domain.Submitted {
			continue
		}
		item := waiting[document.DriverID]
		item.Waiting++
		if item.Since.IsZero() || document.CreatedAt.Before(item.Since) {
			item.Since = document.CreatedAt
		}
		item.Driver = m.drivers[document.DriverID]
		waiting[document.DriverID] = item
	}

	items := make([]service.QueueItem, 0, len(waiting))
	for _, item := range waiting {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Since.Equal(items[j].Since) {
			return items[i].Driver.ID < items[j].Driver.ID
		}
		return items[i].Since.Before(items[j].Since)
	})

	if cursor != "" {
		for i, item := range items {
			if item.Driver.ID == cursor {
				items = items[i+1:]
				break
			}
		}
	}

	page := service.QueuePage{}
	if len(items) > limit {
		items = items[:limit]
		page.NextCursor = items[limit-1].Driver.ID
	}
	page.Items = items
	return page, nil
}

func (m *Memory) ExpireDocuments(_ context.Context, now time.Time) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	seen := make(map[string]bool)
	var drivers []string
	for id, document := range m.documents {
		if document.Status != domain.Approved || document.ExpiresAt.IsZero() || now.Before(document.ExpiresAt) {
			continue
		}
		document.Status = domain.Expired
		document.UpdatedAt = now
		m.documents[id] = document
		if !seen[document.DriverID] {
			seen[document.DriverID] = true
			drivers = append(drivers, document.DriverID)
		}
	}
	sort.Strings(drivers)
	return drivers, nil
}
