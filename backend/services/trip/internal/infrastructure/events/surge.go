package events

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ishakdeveloper/surge/shared/geo"
	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/ishakdeveloper/surge/shared/wire"
	"github.com/twmb/franz-go/pkg/kgo"
)

// SurgeMaxAge is how long a multiplier stays believable. The matcher that owns
// a surging cell republishes it every 30s, so silence longer than this means
// that matcher has stopped — and a dead matcher must not leave a price stuck
// high.
const SurgeMaxAge = 2 * time.Minute

// SurgeBook is the trip service's view of surge: the latest multiplier per
// resolution-7 cell, from the compacted `surge.cells` topic.
//
// Read without a consumer group and from the start, by every instance: each
// one needs every cell, and a compacted topic read from its start is the
// current price list rather than a history of it.
type SurgeBook struct {
	client *kgo.Client
	now    func() time.Time

	mu    sync.RWMutex
	cells map[string]wire.CellSurge
}

func NewSurgeBook(brokers []string) (*SurgeBook, error) {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ConsumeTopics(kafkax.TopicSurgeCells),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	if err != nil {
		return nil, fmt.Errorf("trip: surge reader: %w", err)
	}
	book := NewMemorySurgeBook(time.Now)
	book.client = client
	return book, nil
}

// NewMemorySurgeBook is a book with no topic behind it, fed through Record.
func NewMemorySurgeBook(now func() time.Time) *SurgeBook {
	return &SurgeBook{now: now, cells: map[string]wire.CellSurge{}}
}

func (b *SurgeBook) Close() {
	if b.client != nil {
		b.client.Close()
	}
}

func (b *SurgeBook) Run(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		fetches := b.client.PollRecords(ctx, 1000)
		if fetches.IsClientClosed() {
			return nil
		}
		fetches.EachRecord(func(record *kgo.Record) {
			var surge wire.CellSurge
			if err := json.Unmarshal(record.Value, &surge); err != nil || surge.Tag != wire.TagCellSurge {
				return
			}
			b.Record(surge)
		})
	}
}

// Record keeps the newest price for a cell. Reading a compacted topic from the
// start can deliver an older record after a newer one across a replay, and the
// timestamp is what decides.
func (b *SurgeBook) Record(surge wire.CellSurge) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if current, ok := b.cells[surge.Cell]; ok && current.AtMs > surge.AtMs {
		return
	}
	b.cells[surge.Cell] = surge
}

// MultiplierAt is the surge on a pickup: its resolution-7 cell's latest
// multiplier, or 1.0 when that cell has none, or none recent enough to trust.
func (b *SurgeBook) MultiplierAt(_ context.Context, point geo.Point) float64 {
	cell, err := geo.ShardCell(point)
	if err != nil {
		return 1
	}
	b.mu.RLock()
	surge, ok := b.cells[cell.String()]
	b.mu.RUnlock()
	if !ok || surge.Multiplier < 1 || b.now().Sub(time.UnixMilli(surge.AtMs)) > SurgeMaxAge {
		return 1
	}
	return surge.Multiplier
}
