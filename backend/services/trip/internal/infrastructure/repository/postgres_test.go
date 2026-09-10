package repository_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/trip/internal/domain"
	"github.com/ishakdeveloper/surge/services/trip/internal/infrastructure/repository"
	"github.com/ishakdeveloper/surge/shared/geo"
	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/ishakdeveloper/surge/shared/pgtest"
	"github.com/ishakdeveloper/surge/shared/wire"
	"github.com/jackc/pgx/v5/pgxpool"
)

var booked = time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

func newTrip(id, key string) *domain.Trip {
	return &domain.Trip{
		ID: id, RiderID: "rider-1", Status: domain.StatusRequested,
		Pickup:  geo.Point{Lat: 52.3791, Lng: 4.9003},
		Dropoff: geo.Point{Lat: 52.3600, Lng: 4.8852},
		Meters:  6840, Seconds: 721,
		TotalCents: 1450, Currency: domain.MarketCurrency,
		SurgeMultiplier: 1, PackageSlug: "sedan",
		IdempotencyKey: key, CreatedAt: booked, UpdatedAt: booked,
	}
}

// published reads the outbox as the relay would, and holds each row to the
// topic and key the facts must travel under.
func published(t *testing.T, pool *pgxpool.Pool) []wire.TripFact {
	t.Helper()

	rows, err := pool.Query(context.Background(), `select topic, key, value from trip_outbox order by id`)
	if err != nil {
		t.Fatalf("read outbox: %v", err)
	}
	defer rows.Close()

	var facts []wire.TripFact
	for rows.Next() {
		var (
			topic, key string
			value      []byte
			fact       wire.TripFact
		)
		if err := rows.Scan(&topic, &key, &value); err != nil {
			t.Fatalf("scan outbox: %v", err)
		}
		if err := json.Unmarshal(value, &fact); err != nil {
			t.Fatalf("decode outbox row: %v", err)
		}
		if topic != kafkax.TopicTripLifecycle || key != fact.TripID {
			t.Errorf("fact for %s written to %s keyed %q", fact.TripID, topic, key)
		}
		facts = append(facts, fact)
	}
	return facts
}

func TestCreateStoresTheTripAndItsFactTogether(t *testing.T) {
	pool := pgtest.Pool(t)
	repo := repository.NewPostgres(pool)
	ctx := context.Background()

	trip := newTrip("trip-1", "key-1")
	if err := repo.Create(ctx, trip, domain.FactsOf(trip, "")...); err != nil {
		t.Fatalf("create: %v", err)
	}

	stored, err := repo.Get(ctx, trip.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Currency != domain.MarketCurrency || stored.TotalCents != 1450 {
		t.Errorf("stored %d %q", stored.TotalCents, stored.Currency)
	}

	facts := published(t, pool)
	if len(facts) != 1 || facts[0].Tag != wire.FactTripRequested || facts[0].TotalCents != 1450 {
		t.Fatalf("outbox holds %+v, want one request for 1450", facts)
	}

	// A racing retry with the same key: the insert does nothing, and so must
	// the outbox, or the rider's card is held twice for one ride.
	retry := newTrip("trip-2", "key-1")
	if err := repo.Create(ctx, retry, domain.FactsOf(retry, "")...); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := published(t, pool); len(got) != 1 {
		t.Errorf("a retried booking wrote %d facts, want 1", len(got))
	}
	winner, err := repo.FindByIdempotencyKey(ctx, "rider-1", "key-1")
	if err != nil || winner.ID != trip.ID {
		t.Errorf("the key resolves to %v (%v), want the original", winner, err)
	}
}

func TestUpdateIsACompareAndSet(t *testing.T) {
	pool := pgtest.Pool(t)
	repo := repository.NewPostgres(pool)
	ctx := context.Background()

	trip := newTrip("trip-1", "key-1")
	if err := repo.Create(ctx, trip, domain.FactsOf(trip, "")...); err != nil {
		t.Fatalf("create: %v", err)
	}

	// The accept lands first.
	accepted := *trip
	accepted.DriverID = "drv-1"
	if err := accepted.Transition(domain.StatusAccepted, booked); err != nil {
		t.Fatal(err)
	}
	if err := repo.Update(ctx, &accepted, domain.StatusRequested); err != nil {
		t.Fatalf("accept: %v", err)
	}

	// A cancel decided against the trip as it was before the accept must not
	// land, and must not leave its fact behind either.
	stale := *trip
	stale.CancelReason = "rider"
	if err := stale.Transition(domain.StatusCancelled, booked); err != nil {
		t.Fatal(err)
	}
	err := repo.Update(ctx, &stale, domain.StatusRequested, domain.FactsOf(&stale, domain.StatusRequested)...)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("a stale write: want ErrConflict, got %v", err)
	}
	if facts := published(t, pool); len(facts) != 1 {
		t.Errorf("the refused cancel still wrote a fact: %+v", facts)
	}

	// Decided again against fresh state, it lands with its reason.
	fresh, _ := repo.Get(ctx, trip.ID)
	fresh.CancelReason = "rider"
	if err := fresh.Transition(domain.StatusCancelled, booked); err != nil {
		t.Fatal(err)
	}
	if err := repo.Update(ctx, fresh, domain.StatusAccepted, domain.FactsOf(fresh, domain.StatusAccepted)...); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	facts := published(t, pool)
	if len(facts) != 2 || facts[1].Tag != wire.FactTripCancelled || facts[1].Reason != "rider" || facts[1].DriverID != "drv-1" {
		t.Errorf("outbox holds %+v", facts)
	}

	missing := newTrip("nope", "")
	if err := repo.Update(ctx, missing, domain.StatusRequested); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("updating a trip that does not exist: want ErrNotFound, got %v", err)
	}
}
