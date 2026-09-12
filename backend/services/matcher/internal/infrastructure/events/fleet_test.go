package events

import (
	"encoding/json"
	"testing"

	"github.com/ishakdeveloper/surge/shared/wire"
)

// The roster is a fold over a compacted topic, and everything that can go wrong
// with one is here: an order that is not the order things happened, a record
// that is not a fact, and a key that was never written at all.
//
// Internal rather than external, because decoding is half of what is being
// tested and it is not the matcher's business to expose it.

func approved(id string, atMs int64) wire.FleetFact {
	return wire.FleetFact{
		Tag: wire.FactDriverApproved, DriverID: id,
		Plate: "02-JLT-3", PackageSlug: "sedan", AtMs: atMs,
	}
}

func withdrawn(id string, atMs int64) wire.FleetFact {
	return wire.FleetFact{
		Tag: wire.FactDriverWithdrawn, DriverID: id,
		Reason: "insurance expired", AtMs: atMs,
	}
}

func raw(t *testing.T, fact wire.FleetFact) []byte {
	t.Helper()
	payload, err := json.Marshal(fact)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return payload
}

func TestADriverStandsUntilTheFleetSaysOtherwise(t *testing.T) {
	roster := NewMemoryRoster()
	roster.record([]byte("drv-1"), raw(t, approved("drv-1", 1000)))

	if !roster.Approved("drv-1") {
		t.Fatal("an approved driver was not dispatchable")
	}

	roster.record([]byte("drv-1"), raw(t, withdrawn("drv-1", 2000)))

	if roster.Approved("drv-1") {
		t.Fatal("a withdrawn driver was still dispatchable")
	}
}

// Silence is not approval. A driver nobody has said anything about never
// finished onboarding, and the whole point of gating is that they are not sent
// to a rider while that is true.
func TestADriverTheFleetHasNeverMentionedIsNotApproved(t *testing.T) {
	roster := NewMemoryRoster()
	roster.record([]byte("drv-1"), raw(t, approved("drv-1", 1000)))

	if roster.Approved("drv-2") {
		t.Fatal("a driver the fleet has never mentioned was dispatchable")
	}
}

// A tombstone is the fleet forgetting a driver, and forgetting is not
// approving. The safe reading of a record that no longer exists is the same as
// one that never did.
func TestAForgottenDriverComesOffTheRoster(t *testing.T) {
	roster := NewMemoryRoster()
	roster.record([]byte("drv-1"), raw(t, approved("drv-1", 1000)))
	roster.record([]byte("drv-1"), nil)

	if roster.Approved("drv-1") {
		t.Fatal("a driver whose record was deleted was still dispatchable")
	}
	if roster.Size() != 0 {
		t.Fatalf("expected an empty roster, got %d", roster.Size())
	}
}

// Reading a compacted topic from the start can deliver an older record after a
// newer one across a replay. Applying it would reinstate a driver the fleet
// withdrew this morning.
func TestAnOlderStandingArrivingLateIsIgnored(t *testing.T) {
	roster := NewMemoryRoster()
	roster.record([]byte("drv-1"), raw(t, withdrawn("drv-1", 2000)))
	roster.record([]byte("drv-1"), raw(t, approved("drv-1", 1000)))

	if roster.Approved("drv-1") {
		t.Fatal("a stale approval overwrote the withdrawal that followed it")
	}
}

// Nothing on this topic is trusted to be a fact: half a record is not one, and
// neither is an approval that names no car to dispatch. Neither may change a
// standing.
func TestNothingThatIsNotAFactChangesAStanding(t *testing.T) {
	roster := NewMemoryRoster()
	roster.record([]byte("drv-1"), raw(t, approved("drv-1", 1000)))

	roster.record([]byte("drv-1"), []byte("{"))
	roster.record([]byte("drv-1"), raw(t, wire.FleetFact{
		Tag: wire.FactDriverApproved, DriverID: "drv-1", AtMs: 3000,
	}))

	if !roster.Approved("drv-1") {
		t.Fatal("a malformed record withdrew an approved driver")
	}
	if roster.Size() != 1 {
		t.Fatalf("expected one driver on the roster, got %d", roster.Size())
	}
}
