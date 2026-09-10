package service

import (
	"time"

	"github.com/ishakdeveloper/surge/ingest/internal/domain"
	"github.com/ishakdeveloper/surge/shared/wire"
)

// Translate turns an indexed ping into the events the matcher shards consume.
//
// This is the re-keying step, and it is where the system changes its mind about
// what a message is *about*. On `loc.ping` a record is about a driver, keyed by
// driver id so one driver's reports stay ordered. On `geo.events` a record is
// about a place, keyed by shard cell so one shard's events stay ordered. Ingest
// exists to perform that translation.
//
// A shard crossing produces TWO events on two different partitions — a leave to
// the old owner and an entry to the new one — and nothing orders them relative
// to each other. That is not a flaw to be engineered away: making it one
// message would mean one partition, which would mean one owner, which is the
// sharding gone. The sequence number is what makes the unordered pair safe.
func Translate(observation domain.Observation, now time.Time) []wire.GeoEvent {
	if observation.Stale {
		return nil
	}

	entry := observation.Entry
	atMs := now.UnixMilli()

	shard := entry.Shard.String()
	index := entry.Index.String()

	if observation.New || observation.ShardChanged {
		entered := wire.GeoEvent{
			Tag:  wire.TagDriverEnteredCell,
			Cell: shard,
			AtMs: atMs,
			Entered: &wire.DriverEnteredPayload{
				DriverID:  entry.DriverID,
				Epoch:     entry.Epoch,
				Seq:       entry.Seq,
				Lat:       entry.Point.Lat,
				Lng:       entry.Point.Lng,
				Heading:   entry.Heading,
				Status:    entry.Status,
				IndexCell: index,
				SentAtMs:  entry.SentAtMs,
			},
		}

		if !observation.ShardChanged {
			// A driver appearing from nowhere: no previous owner to tell.
			return []wire.GeoEvent{entered}
		}

		previous := observation.PreviousShard.String()
		entered.Entered.From = previous

		return []wire.GeoEvent{
			entered,
			{
				Tag:  wire.TagDriverLeftCell,
				Cell: previous,
				AtMs: atMs,
				Left: &wire.DriverLeftPayload{
					DriverID: entry.DriverID,
					Epoch:    entry.Epoch,
					Seq:      entry.Seq,
					To:       shard,
				},
			},
		}
	}

	// The common case by two orders of magnitude: a position update inside a
	// cell the shard already owns.
	return []wire.GeoEvent{{
		Tag:  wire.TagDriverMoved,
		Cell: shard,
		AtMs: atMs,
		Moved: &wire.DriverMovedPayload{
			DriverID:  entry.DriverID,
			Epoch:     entry.Epoch,
			Seq:       entry.Seq,
			Lat:       entry.Point.Lat,
			Lng:       entry.Point.Lng,
			Heading:   entry.Heading,
			Status:    entry.Status,
			IndexCell: index,
			SentAtMs:  entry.SentAtMs,
		},
	}}
}
