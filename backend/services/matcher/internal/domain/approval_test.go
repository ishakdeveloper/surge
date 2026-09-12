package domain_test

import (
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/matcher/internal/domain"
	"github.com/ishakdeveloper/surge/shared/wire"
)

// Gating dispatch on what the fleet says.
//
// A driver standing in a cell and pinging is not the same thing as a driver who
// may be sent to a rider, and this is the only place in the system that knows
// the difference. The fleet derives approval from papers; the matcher reads the
// answer and refuses to dispatch anybody else.

// approving is a config that dispatches exactly the drivers named.
func approving(ids ...string) domain.Config {
	approved := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		approved[id] = struct{}{}
	}
	config := domain.DefaultConfig()
	config.Approved = func(driverID string) bool {
		_, ok := approved[driverID]
		return ok
	}
	return config
}

func TestOnlyAnApprovedDriverIsOffered(t *testing.T) {
	shard := domain.NewShard(0, approving("drv-vetted"))

	// The unvetted one is nearer, so nothing but the gate can explain the
	// choice — ranking is by distance and would otherwise pick them.
	must(t, shard, entered(t, "drv-unvetted", dam, 1), base)
	must(t, shard, entered(t, "drv-vetted", nearby, 1), base)

	outcome := must(t, shard, request(t, "trip-a", dam), base)

	if len(outcome.Offers) != 1 {
		t.Fatalf("expected the vetted driver to be offered the trip, got %+v", outcome.Offers)
	}
	if outcome.Offers[0].DriverID != "drv-vetted" {
		t.Fatalf("dispatched %s, who the fleet has not approved", outcome.Offers[0].DriverID)
	}
}

// A city full of cars and nobody to send: the rider is told, rather than left
// waiting on an offer that will never be made.
func TestARiderIsGivenUpOnWhenNobodyNearbyIsApproved(t *testing.T) {
	shard := domain.NewShard(0, approving())
	must(t, shard, entered(t, "drv-1", nearby, 1), base)

	outcome := must(t, shard, request(t, "trip-a", dam), base)

	if len(outcome.Offers) != 0 {
		t.Fatalf("dispatched an unapproved driver: %+v", outcome.Offers)
	}
	if len(outcome.Abandoned) != 1 || outcome.Abandoned[0] != "trip-a" {
		t.Fatalf("expected the request to be abandoned, got %+v", outcome.Abandoned)
	}
}

// The case the predicate exists for rather than a set: approval changes under
// the shard, with no event reaching this partition to say so. A licence expires
// at midnight and the next request must already know.
func TestApprovalWithdrawnUnderTheShardStopsTheNextDispatch(t *testing.T) {
	standing := true
	config := domain.DefaultConfig()
	config.Approved = func(string) bool { return standing }

	shard := domain.NewShard(0, config)
	must(t, shard, entered(t, "drv-1", nearby, 1), base)

	if outcome := must(t, shard, request(t, "trip-a", dam), base); len(outcome.Offers) != 1 {
		t.Fatalf("expected an offer while the driver was approved, got %+v", outcome.Offers)
	}

	standing = false
	outcome := must(t, shard, request(t, "trip-b", dam), base.Add(time.Minute))

	if len(outcome.Offers) != 0 {
		t.Fatalf("dispatched a driver whose papers had lapsed: %+v", outcome.Offers)
	}
}

// The cross-shard half. The shard that holds the driver answers, because it is
// the one whose answer is acted on, and it says why.
func TestACrossShardReservationOfAnUnapprovedDriverIsRefused(t *testing.T) {
	shard := domain.NewShard(0, approving())
	must(t, shard, entered(t, "drv-1", nearby, 1), base)

	answer := must(t, shard, wire.GeoEvent{
		Tag:  wire.TagReserveDriver,
		Cell: shardCell(t, nearby),
		Reserve: &wire.ReservePayload{
			DriverID: "drv-1", TripID: "trip-remote",
			ReplyCell:  "8919695306bffff",
			DeadlineMs: base.Add(8 * time.Second).UnixMilli(),
			PickupLat:  dam.Lat, PickupLng: dam.Lng,
		},
	}, base)

	if len(answer.Offers) != 0 {
		t.Fatalf("an unapproved driver was dispatched across a shard boundary: %+v", answer.Offers)
	}
	if len(answer.GeoEvents) != 1 || answer.GeoEvents[0].Reserved.Reason != wire.RejectUnapproved {
		t.Fatalf("expected an unapproved refusal, got %+v", answer.GeoEvents)
	}
	// Worth its own reason rather than folding into "unavailable": the metric
	// is how a rising count of expiring paperwork is told from cars going home.
	if len(answer.Rejections) != 1 || answer.Rejections[0] != wire.RejectUnapproved {
		t.Fatalf("expected the refusal to be counted as unapproved, got %+v", answer.Rejections)
	}
}

// Nothing configured is everything allowed, which is what the simulator's
// invented fleet and every other test in this package rely on.
func TestNoApprovalRuleDispatchesEveryone(t *testing.T) {
	shard := domain.NewShard(0, domain.DefaultConfig())
	must(t, shard, entered(t, "drv-1", nearby, 1), base)

	if outcome := must(t, shard, request(t, "trip-a", dam), base); len(outcome.Offers) != 1 {
		t.Fatalf("expected an offer with no rule configured, got %+v", outcome.Offers)
	}
}
