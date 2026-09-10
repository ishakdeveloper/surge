package matcher_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/internal/matcher"
	"github.com/ishakdeveloper/surge/pkg/geo"
	"github.com/ishakdeveloper/surge/pkg/wire"
)

var (
	dam    = geo.Point{Lat: 52.3730, Lng: 4.8926}
	nearby = geo.Point{Lat: 52.3735, Lng: 4.8930}
	base   = time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
)

func shardCell(t *testing.T, point geo.Point) string {
	t.Helper()
	cell, err := geo.ShardCell(point)
	if err != nil {
		t.Fatalf("shard cell: %v", err)
	}
	return cell.String()
}

func indexCell(t *testing.T, point geo.Point) string {
	t.Helper()
	cell, err := geo.IndexCell(point)
	if err != nil {
		t.Fatalf("index cell: %v", err)
	}
	return cell.String()
}

func entered(t *testing.T, id string, point geo.Point, seq uint64) wire.GeoEvent {
	t.Helper()
	return wire.GeoEvent{
		Tag:  wire.TagDriverEnteredCell,
		Cell: shardCell(t, point),
		AtMs: base.UnixMilli(),
		Entered: &wire.DriverEnteredPayload{
			DriverID: id, Seq: seq,
			Lat: point.Lat, Lng: point.Lng,
			Status: wire.StatusIdle, IndexCell: indexCell(t, point),
		},
	}
}

func request(t *testing.T, trip string, pickup geo.Point) wire.GeoEvent {
	t.Helper()
	return wire.GeoEvent{
		Tag:  wire.TagMatchRequested,
		Cell: shardCell(t, pickup),
		AtMs: base.UnixMilli(),
		Requested: &wire.MatchRequestPayload{
			TripID: trip, RiderID: "rider-" + trip,
			PickupLat: pickup.Lat, PickupLng: pickup.Lng,
			DropLat: 52.36, DropLng: 4.88,
			RequestedAtMs:  base.UnixMilli(),
			IdempotencyKey: "key-" + trip,
		},
	}
}

func must(t *testing.T, shard *matcher.Shard, event wire.GeoEvent, now time.Time) matcher.Outcome {
	t.Helper()
	outcome, err := shard.Handle(event, now)
	if err != nil {
		t.Fatalf("handle %s: %v", event.Tag, err)
	}
	return outcome
}

// The reason this whole architecture exists.
//
// Two riders, one nearby driver, arriving as two events on one partition. The
// textbook answer is a distributed lock. Here there is nothing to lock: both
// requests are serialised through one goroutine, so the second one simply finds
// the driver already spoken for. If this test ever fails, the sharding is not
// buying what it is supposed to buy.
func TestTwoRidersOneDriver(t *testing.T) {
	shard := matcher.NewShard(0, matcher.DefaultConfig())
	must(t, shard, entered(t, "drv-1", nearby, 1), base)

	first := must(t, shard, request(t, "trip-a", dam), base)
	second := must(t, shard, request(t, "trip-b", dam), base)

	if len(first.Offers) != 1 {
		t.Fatalf("the first rider should get the driver, got %d offers", len(first.Offers))
	}
	if first.Offers[0].DriverID != "drv-1" {
		t.Fatalf("offered %q, want drv-1", first.Offers[0].DriverID)
	}

	if len(second.Offers) != 0 {
		t.Fatalf("the driver was offered to two riders at once: %+v", second.Offers)
	}
	if len(second.Abandoned) != 1 || second.Abandoned[0] != "trip-b" {
		t.Fatalf("the second rider should be abandoned with nobody left, got %+v", second)
	}
}

// The other half: once the first rider's offer resolves as a decline, the
// driver becomes available and a later request can have them.
func TestDeclinedOfferReleasesTheDriver(t *testing.T) {
	shard := matcher.NewShard(0, matcher.DefaultConfig())
	must(t, shard, entered(t, "drv-1", nearby, 1), base)

	first := must(t, shard, request(t, "trip-a", dam), base)
	offer := first.Offers[0]

	declined := must(t, shard, wire.GeoEvent{
		Tag:  wire.TagOfferReplied,
		Cell: offer.ReplyCell,
		AtMs: base.UnixMilli(),
		Replied: &wire.OfferRepliedPayload{
			TripID: "trip-a", DriverID: "drv-1", Accepted: false,
		},
	}, base.Add(time.Second))

	// No other candidate, so this request gives up — but the driver is free.
	if len(declined.Abandoned) != 1 {
		t.Fatalf("expected the request to be abandoned, got %+v", declined)
	}

	second := must(t, shard, request(t, "trip-b", dam), base.Add(2*time.Second))
	if len(second.Offers) != 1 || second.Offers[0].DriverID != "drv-1" {
		t.Fatalf("a declined driver should be available again, got %+v", second)
	}
}

func TestAcceptedOfferMatches(t *testing.T) {
	shard := matcher.NewShard(0, matcher.DefaultConfig())
	must(t, shard, entered(t, "drv-1", nearby, 1), base)

	first := must(t, shard, request(t, "trip-a", dam), base)
	offer := first.Offers[0]

	accepted := must(t, shard, wire.GeoEvent{
		Tag:  wire.TagOfferReplied,
		Cell: offer.ReplyCell,
		Replied: &wire.OfferRepliedPayload{
			TripID: "trip-a", DriverID: "drv-1", Accepted: true,
		},
	}, base.Add(3*time.Second))

	if len(accepted.Matched) != 1 {
		t.Fatalf("expected a match, got %+v", accepted)
	}
	// Latency is measured from when the rider asked, not from when the shard
	// got round to it — three seconds here, including the driver's thinking.
	if accepted.Matched[0].Latency != 3*time.Second {
		t.Errorf("latency %v, want 3s measured from the request", accepted.Matched[0].Latency)
	}
}

// At-least-once delivery is the normal case, not the edge case: a rebalance or
// a failed commit redelivers. Dispatching twice would send two drivers.
func TestRedeliveredRequestDoesNotDoubleDispatch(t *testing.T) {
	shard := matcher.NewShard(0, matcher.DefaultConfig())
	must(t, shard, entered(t, "drv-1", nearby, 1), base)
	must(t, shard, entered(t, "drv-2", nearby, 1), base)

	first := must(t, shard, request(t, "trip-a", dam), base)
	replay := must(t, shard, request(t, "trip-a", dam), base)

	if len(first.Offers) != 1 {
		t.Fatalf("expected one offer, got %d", len(first.Offers))
	}
	if len(replay.Offers) != 0 {
		t.Fatalf("a redelivered request dispatched a second driver: %+v", replay.Offers)
	}
}

// The two halves of a handover travel on different partitions, so nothing
// orders them. Arriving reversed must not delete a driver who is present.
func TestReversedHandoverDoesNotLoseTheDriver(t *testing.T) {
	shard := matcher.NewShard(0, matcher.DefaultConfig())

	// The driver arrives with sequence 7 …
	must(t, shard, entered(t, "drv-1", nearby, 7), base)

	// … and the leave for sequence 5, from an earlier crossing, turns up after.
	must(t, shard, wire.GeoEvent{
		Tag:  wire.TagDriverLeftCell,
		Cell: shardCell(t, nearby),
		Left: &wire.DriverLeftPayload{DriverID: "drv-1", Seq: 5, To: "somewhere"},
	}, base)

	if shard.Drivers() != 1 {
		t.Fatal("a stale leave evicted a driver who had already come back")
	}

	matched := must(t, shard, request(t, "trip-a", dam), base)
	if len(matched.Offers) != 1 {
		t.Fatalf("the driver should still be matchable, got %+v", matched)
	}
}

// A reservation is a promise to a rider. Geography changing does not cancel it;
// only the offer's own deadline does.
func TestLeavingTheCellDoesNotReleaseAReservedDriver(t *testing.T) {
	shard := matcher.NewShard(0, matcher.DefaultConfig())
	must(t, shard, entered(t, "drv-1", nearby, 1), base)
	must(t, shard, request(t, "trip-a", dam), base)

	must(t, shard, wire.GeoEvent{
		Tag:  wire.TagDriverLeftCell,
		Cell: shardCell(t, nearby),
		Left: &wire.DriverLeftPayload{DriverID: "drv-1", Seq: 2, To: "elsewhere"},
	}, base.Add(time.Second))

	if shard.Offers() != 1 {
		t.Fatal("the offer was dropped when the driver crossed a boundary")
	}
	if shard.Drivers() != 1 {
		t.Fatal("a reserved driver was evicted mid-offer")
	}
}

// Every hold has a deadline, because the alternative is a driver stranded by a
// rider who closed their laptop.
func TestOfferExpiryReleasesTheDriverAndMovesOn(t *testing.T) {
	config := matcher.DefaultConfig()
	config.OfferTTL = 5 * time.Second

	shard := matcher.NewShard(0, config)
	must(t, shard, entered(t, "drv-1", nearby, 1), base)
	must(t, shard, entered(t, "drv-2", nearby, 1), base)

	first := must(t, shard, request(t, "trip-a", dam), base)
	offered := first.Offers[0].DriverID

	expired := shard.Tick(base.Add(6 * time.Second))

	if len(expired.Rejections) == 0 || expired.Rejections[0] != wire.RejectTimeout {
		t.Fatalf("expected a timeout rejection, got %+v", expired.Rejections)
	}
	// The request is not abandoned — it moves to the second candidate.
	if len(expired.Offers) != 1 {
		t.Fatalf("expected the request to try the next driver, got %+v", expired)
	}
	if expired.Offers[0].DriverID == offered {
		t.Error("the same driver was offered the trip twice in a row")
	}
}

// An offer that has already timed out must not be revived by a late accept, or
// the rider gets a driver the system has already given away.
func TestLateAcceptDoesNotResurrectAnExpiredOffer(t *testing.T) {
	config := matcher.DefaultConfig()
	config.OfferTTL = 5 * time.Second

	shard := matcher.NewShard(0, config)
	must(t, shard, entered(t, "drv-1", nearby, 1), base)

	first := must(t, shard, request(t, "trip-a", dam), base)
	offer := first.Offers[0]

	shard.Tick(base.Add(6 * time.Second))

	late := must(t, shard, wire.GeoEvent{
		Tag:  wire.TagOfferReplied,
		Cell: offer.ReplyCell,
		Replied: &wire.OfferRepliedPayload{
			TripID: "trip-a", DriverID: "drv-1", Accepted: true,
		},
	}, base.Add(7*time.Second))

	if len(late.Matched) != 0 {
		t.Fatalf("an expired offer was matched after the fact: %+v", late.Matched)
	}
}

// The cross-shard half, from the owning side. Contention across a boundary
// resolves the same way as contention within one: serially, at the owner.
func TestCrossShardReservationRefusesABusyDriver(t *testing.T) {
	shard := matcher.NewShard(0, matcher.DefaultConfig())
	must(t, shard, entered(t, "drv-1", nearby, 1), base)

	// Our own rider takes the driver first.
	must(t, shard, request(t, "trip-local", dam), base)

	// A neighbouring shard then asks for the same driver.
	answer := must(t, shard, wire.GeoEvent{
		Tag:  wire.TagReserveDriver,
		Cell: shardCell(t, nearby),
		Reserve: &wire.ReservePayload{
			DriverID: "drv-1", TripID: "trip-remote",
			ReplyCell:  "8919695306bffff",
			DeadlineMs: base.Add(8 * time.Second).UnixMilli(),
			PickupLat:  dam.Lat, PickupLng: dam.Lng,
		},
	}, base.Add(time.Second))

	if len(answer.Offers) != 0 {
		t.Fatal("a driver already on an offer was dispatched a second one")
	}
	if len(answer.GeoEvents) != 1 {
		t.Fatalf("expected a refusal to be sent back, got %+v", answer.GeoEvents)
	}

	result := answer.GeoEvents[0]
	if result.Tag != wire.TagReserveResult || result.Reserved.OK {
		t.Fatalf("expected a rejection, got %+v", result.Reserved)
	}
	if result.Reserved.Reason != wire.RejectBusy {
		t.Errorf("reason %q, want %q", result.Reserved.Reason, wire.RejectBusy)
	}
	// Addressed back to whoever asked, not broadcast.
	if result.Cell != "8919695306bffff" {
		t.Errorf("refusal sent to %q, want the requester's cell", result.Cell)
	}
}

func TestReservationForAnUnknownDriverIsRefusedNotDropped(t *testing.T) {
	shard := matcher.NewShard(0, matcher.DefaultConfig())

	answer := must(t, shard, wire.GeoEvent{
		Tag:  wire.TagReserveDriver,
		Cell: shardCell(t, nearby),
		Reserve: &wire.ReservePayload{
			DriverID: "drv-ghost", TripID: "trip-x",
			ReplyCell:  "8919695306bffff",
			DeadlineMs: base.Add(8 * time.Second).UnixMilli(),
		},
	}, base)

	// Silence would strand the requesting rider until their own request TTL.
	if len(answer.GeoEvents) != 1 || answer.GeoEvents[0].Reserved.Reason != wire.RejectUnknownDriver {
		t.Fatalf("expected an unknown-driver refusal, got %+v", answer.GeoEvents)
	}
}

func TestNoDriversMeansAbandoned(t *testing.T) {
	shard := matcher.NewShard(0, matcher.DefaultConfig())

	outcome := must(t, shard, request(t, "trip-a", dam), base)
	if len(outcome.Abandoned) != 1 {
		t.Fatalf("a request with no drivers should be abandoned, got %+v", outcome)
	}
}

// Contention at scale: many riders, few drivers, all through one loop. Every
// driver must end up held by exactly one trip.
func TestNoDriverIsEverDoubleBooked(t *testing.T) {
	shard := matcher.NewShard(0, matcher.DefaultConfig())

	const drivers = 20
	for i := range drivers {
		must(t, shard, entered(t, fmt.Sprintf("drv-%d", i), nearby, 1), base)
	}

	held := map[string]string{}
	for i := range 100 {
		trip := fmt.Sprintf("trip-%d", i)
		outcome := must(t, shard, request(t, trip, dam), base)

		for _, offer := range outcome.Offers {
			if existing, taken := held[offer.DriverID]; taken {
				t.Fatalf("%s offered to both %s and %s", offer.DriverID, existing, offer.TripID)
			}
			held[offer.DriverID] = offer.TripID
		}
	}

	if len(held) != drivers {
		t.Errorf("%d of %d drivers were dispatched; the rest were never offered", len(held), drivers)
	}
}

// A rebalance moves a partition mid-offer. What has to survive is the promise —
// which driver is held for which trip — because nothing else in the system
// remembers it. What must NOT be carried over is driver positions: they arrive
// again on their own, and writing them down would be ten thousand records a
// second to buy four seconds.
func TestCheckpointCarriesPromisesAndNotPositions(t *testing.T) {
	losing := matcher.NewShard(0, matcher.DefaultConfig())
	must(t, losing, entered(t, "drv-1", nearby, 1), base)
	first := must(t, losing, request(t, "trip-a", dam), base)

	if len(first.Offers) != 1 {
		t.Fatalf("setup: expected an offer, got %+v", first)
	}

	records := losing.Checkpoint(base.Add(time.Second))
	if len(records) == 0 {
		t.Fatal("an outstanding offer produced no checkpoint")
	}

	held := 0
	for _, record := range records {
		held += len(record.Offers)
	}
	if held != 1 {
		t.Fatalf("checkpoint carries %d offers, want 1", held)
	}

	// The partition moves to another instance.
	gaining := matcher.NewShard(0, matcher.DefaultConfig())
	gaining.Restore(records, base.Add(2*time.Second))

	if gaining.Offers() != 1 {
		t.Fatalf("the promise did not survive the handover, %d offers restored", gaining.Offers())
	}
	// Positions are not restored, deliberately.
	if gaining.Drivers() != 0 {
		t.Errorf("driver positions were carried over; they should come from the ping stream")
	}
	if gaining.RestoredHolds() != 1 {
		t.Errorf("the hold should be waiting for its driver to reappear, got %d", gaining.RestoredHolds())
	}

	// When the driver's next ping arrives, the hold is applied — so the new
	// owner will not hand them to somebody else.
	must(t, gaining, entered(t, "drv-1", nearby, 2), base.Add(3*time.Second))
	if gaining.RestoredHolds() != 0 {
		t.Error("the hold was not applied when the driver reappeared")
	}

	taken := must(t, gaining, request(t, "trip-b", dam), base.Add(4*time.Second))
	if len(taken.Offers) != 0 {
		t.Fatalf("a driver reserved before the rebalance was re-offered after it: %+v", taken.Offers)
	}
}

// A hold whose driver never comes back must lapse rather than leak. Otherwise a
// rebalance during an offer permanently removes a driver from the pool.
func TestRestoredHoldExpiresIfTheDriverNeverReturns(t *testing.T) {
	config := matcher.DefaultConfig()
	config.OfferTTL = 5 * time.Second

	losing := matcher.NewShard(0, config)
	must(t, losing, entered(t, "drv-1", nearby, 1), base)
	must(t, losing, request(t, "trip-a", dam), base)

	gaining := matcher.NewShard(0, config)
	gaining.Restore(losing.Checkpoint(base.Add(time.Second)), base.Add(time.Second))

	if gaining.Offers() != 1 {
		t.Fatalf("setup: expected a restored offer, got %d", gaining.Offers())
	}

	// The offer's own deadline still runs across the handover.
	gaining.Tick(base.Add(10 * time.Second))

	if gaining.Offers() != 0 {
		t.Error("the restored offer outlived its deadline")
	}
	if gaining.RestoredHolds() != 0 {
		t.Error("the hold leaked after its offer expired")
	}
}

// Compaction keeps the last record per key, so a cell that no longer has offers
// needs an empty record to supersede its previous one — otherwise a restoring
// shard reinstates offers that were resolved before the handover.
func TestCheckpointSupersedesResolvedOffers(t *testing.T) {
	shard := matcher.NewShard(0, matcher.DefaultConfig())
	must(t, shard, entered(t, "drv-1", nearby, 1), base)
	first := must(t, shard, request(t, "trip-a", dam), base)

	shard.Checkpoint(base.Add(time.Second))

	must(t, shard, wire.GeoEvent{
		Tag:  wire.TagOfferReplied,
		Cell: first.Offers[0].ReplyCell,
		Replied: &wire.OfferRepliedPayload{
			TripID: "trip-a", DriverID: "drv-1", Accepted: true,
		},
	}, base.Add(2*time.Second))

	records := shard.Checkpoint(base.Add(3 * time.Second))

	if len(records) == 0 {
		t.Fatal("a cell with a previous checkpoint must still be written")
	}
	for _, record := range records {
		if len(record.Offers) != 0 {
			t.Fatalf("a resolved offer is still checkpointed: %+v", record.Offers)
		}
	}
}
