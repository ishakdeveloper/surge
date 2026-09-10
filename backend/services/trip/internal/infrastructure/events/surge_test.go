package events_test

import (
	"context"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/trip/internal/infrastructure/events"
	"github.com/ishakdeveloper/surge/shared/geo"
	"github.com/ishakdeveloper/surge/shared/wire"
)

var (
	centraal = geo.Point{Lat: 52.3791, Lng: 4.9003}
	noon     = time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
)

func cellOf(t *testing.T, point geo.Point) string {
	t.Helper()
	cell, err := geo.ShardCell(point)
	if err != nil {
		t.Fatalf("cell: %v", err)
	}
	return cell.String()
}

func TestAPickupPaysItsCellsSurge(t *testing.T) {
	book := events.NewMemorySurgeBook(func() time.Time { return noon })
	book.Record(wire.CellSurge{Tag: wire.TagCellSurge, Cell: cellOf(t, centraal), Multiplier: 1.6, AtMs: noon.UnixMilli()})

	if got := book.MultiplierAt(context.Background(), centraal); got != 1.6 {
		t.Errorf("multiplier at Centraal = %v, want 1.6", got)
	}
	if got := book.MultiplierAt(context.Background(), geo.Point{Lat: 52.33, Lng: 4.82}); got != 1 {
		t.Errorf("a cell with no surge = %v, want 1.0", got)
	}
}

// A matcher that stopped publishing must not leave a price stuck high.
func TestAStalePriceIsBasePrice(t *testing.T) {
	now := noon
	book := events.NewMemorySurgeBook(func() time.Time { return now })
	book.Record(wire.CellSurge{Tag: wire.TagCellSurge, Cell: cellOf(t, centraal), Multiplier: 2.4, AtMs: noon.UnixMilli()})

	now = noon.Add(events.SurgeMaxAge + time.Second)
	if got := book.MultiplierAt(context.Background(), centraal); got != 1 {
		t.Errorf("a %s-old price = %v, want 1.0", events.SurgeMaxAge+time.Second, got)
	}
}

// The newest price wins, whatever order the records arrive in.
func TestAnOlderRecordDoesNotOverwriteANewerOne(t *testing.T) {
	book := events.NewMemorySurgeBook(func() time.Time { return noon })
	cell := cellOf(t, centraal)
	book.Record(wire.CellSurge{Tag: wire.TagCellSurge, Cell: cell, Multiplier: 1.2, AtMs: noon.UnixMilli()})
	book.Record(wire.CellSurge{Tag: wire.TagCellSurge, Cell: cell, Multiplier: 2.8, AtMs: noon.Add(-time.Minute).UnixMilli()})

	if got := book.MultiplierAt(context.Background(), centraal); got != 1.2 {
		t.Errorf("multiplier = %v, want the newer 1.2", got)
	}
}
