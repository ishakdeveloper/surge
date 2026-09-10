package wire

import (
	"encoding/json"
	"fmt"
)

// The `geo.events` vocabulary — a tagged union, keyed by resolution-7 shard
// cell, carrying everything a matcher shard needs.
//
// One topic rather than four. No Kafka consumer-group balancer guarantees
// co-partitioned assignment across topics: an instance can be handed partition
// 3 of one and partition 5 of another, and then "the shard that owns these
// cells" is no longer a thing that exists. With a single topic, owning a
// partition means owning every event for those cells, in total order, across
// every event type — which is what collapses a shard to one goroutine over
// in-memory state with no locks at all.
//
// The discriminator is `_tag`, not `type`, because these types are destined to
// be generated from Effect `Schema` tagged unions in `packages/domain`, and
// that is the field Effect uses.
const (
	// TagDriverMoved is a position update within the cell the driver already
	// occupies. The high-volume case: most pings do not cross a boundary.
	TagDriverMoved = "DriverMoved"

	// TagDriverEnteredCell and TagDriverLeftCell are the two halves of a
	// handover, and they go to DIFFERENT partitions — that is the whole reason
	// they exist as separate messages rather than as one "moved between cells".
	// Kafka orders within a partition and not across them, so the two can
	// arrive in either order at their respective shards. Seq is what makes that
	// survivable.
	TagDriverEnteredCell = "DriverEnteredCell"
	TagDriverLeftCell    = "DriverLeftCell"

	// TagMatchRequested is a rider asking for a ride, routed to the shard that
	// owns the pickup point.
	TagMatchRequested = "MatchRequested"

	// TagReserveDriver asks the shard that owns a driver to hold them for a
	// trip. Cross-shard, and the reason there is no distributed lock anywhere
	// in this system: the owner processes it serially, so the race cannot form.
	TagReserveDriver = "ReserveDriver"

	// TagReserveResult answers a ReserveDriver, routed back to the requesting
	// shard by its own cell.
	TagReserveResult = "ReserveResult"

	// TagOfferReplied is a driver accepting or declining a dispatched offer.
	TagOfferReplied = "OfferReplied"
)

// GeoEvent is the envelope. Every event carries the shard cell it is addressed
// to, which is also the Kafka partition key — so routing is a property of the
// message rather than of the code that sends it.
type GeoEvent struct {
	Tag string `json:"_tag"`
	// Cell is the resolution-7 cell this event belongs to, as a hex string.
	Cell string `json:"cell"`
	// AtMs is when the event was produced, Unix milliseconds.
	AtMs int64 `json:"atMs"`

	// Exactly one of these is set, per Tag. Pointers rather than a flat struct
	// so that an absent variant is absent rather than zero — a MatchRequested
	// with a zero-valued DriverMoved hanging off it would decode without
	// complaint and be wrong in a way nothing catches.
	Moved     *DriverMovedPayload   `json:"moved,omitempty"`
	Entered   *DriverEnteredPayload `json:"entered,omitempty"`
	Left      *DriverLeftPayload    `json:"left,omitempty"`
	Requested *MatchRequestPayload  `json:"requested,omitempty"`
	Reserve   *ReservePayload       `json:"reserve,omitempty"`
	Reserved  *ReserveResultPayload `json:"reserved,omitempty"`
	Replied   *OfferRepliedPayload  `json:"replied,omitempty"`
}

// DriverMovedPayload is a position update inside a cell the shard already owns.
type DriverMovedPayload struct {
	DriverID  string       `json:"driverId"`
	Seq       uint64       `json:"seq"`
	Lat       float64      `json:"lat"`
	Lng       float64      `json:"lng"`
	Heading   float64      `json:"heading"`
	Status    DriverStatus `json:"status"`
	IndexCell string       `json:"indexCell"`
	SentAtMs  int64        `json:"sentAtMs"`
}

// DriverEnteredPayload hands a driver to a new shard. It carries the full
// position, because the receiving shard has never seen this driver and cannot
// derive one from anything it holds.
type DriverEnteredPayload struct {
	DriverID  string       `json:"driverId"`
	Seq       uint64       `json:"seq"`
	Lat       float64      `json:"lat"`
	Lng       float64      `json:"lng"`
	Heading   float64      `json:"heading"`
	Status    DriverStatus `json:"status"`
	IndexCell string       `json:"indexCell"`
	SentAtMs  int64        `json:"sentAtMs"`
	// From is the cell being left, for tracing a handover across two partitions.
	From string `json:"from"`
}

// DriverLeftPayload releases a driver from a shard. Deliberately thin: the
// receiving shard needs an identity and a sequence number, nothing else.
type DriverLeftPayload struct {
	DriverID string `json:"driverId"`
	Seq      uint64 `json:"seq"`
	To       string `json:"to"`
}

// MatchRequestPayload is a ride request, addressed to the pickup's shard.
type MatchRequestPayload struct {
	TripID    string  `json:"tripId"`
	RiderID   string  `json:"riderId"`
	PickupLat float64 `json:"pickupLat"`
	PickupLng float64 `json:"pickupLng"`
	DropLat   float64 `json:"dropLat"`
	DropLng   float64 `json:"dropLng"`
	// RequestedAtMs is when the rider asked, which is where match latency is
	// measured from — not from when a shard got round to it.
	RequestedAtMs int64 `json:"requestedAtMs"`
	// IdempotencyKey makes at-least-once delivery safe. A redelivered request
	// with a key the shard has already seen is dropped rather than dispatched a
	// second time.
	IdempotencyKey string `json:"idempotencyKey"`
}

// ReservePayload asks a driver's owning shard to hold them.
type ReservePayload struct {
	DriverID string `json:"driverId"`
	TripID   string `json:"tripId"`
	// ReplyCell is the requesting shard's cell, so the answer can be routed
	// back to whoever asked rather than broadcast.
	ReplyCell string `json:"replyCell"`
	// DeadlineMs bounds the hold. A reservation whose requester died must not
	// strand a driver forever, so the owner expires it on its own.
	DeadlineMs int64 `json:"deadlineMs"`
	// RiderID and pickup travel with the reservation so the owning shard can
	// dispatch the offer itself without a second round trip.
	RiderID   string  `json:"riderId"`
	PickupLat float64 `json:"pickupLat"`
	PickupLng float64 `json:"pickupLng"`
}

// ReserveResultPayload answers a reservation.
type ReserveResultPayload struct {
	DriverID string `json:"driverId"`
	TripID   string `json:"tripId"`
	OK       bool   `json:"ok"`
	// Reason is why not, when OK is false. A closed set so it can be a metric
	// label without unbounded cardinality.
	Reason ReserveRejection `json:"reason,omitempty"`
}

// ReserveRejection is why a reservation failed.
type ReserveRejection string

const (
	// RejectUnknownDriver means the owner has no such driver — usually a
	// handover in flight, and the requester should simply try the next
	// candidate.
	RejectUnknownDriver ReserveRejection = "unknown_driver"
	// RejectBusy means the driver is already reserved or on a trip. This is the
	// contention case, and its rate is the number that says whether the
	// matching strategy is fighting itself.
	RejectBusy ReserveRejection = "busy"
	// RejectUnavailable means the driver is offline.
	RejectUnavailable ReserveRejection = "unavailable"
	// RejectDeclined means the driver was offered the trip and said no. Unlike
	// the others this arrives seconds later, after a round trip to a human.
	RejectDeclined ReserveRejection = "declined"
	// RejectTimeout means the offer expired unanswered. Indistinguishable from
	// a declined offer to the requester, but worth separating in metrics: a
	// rising timeout rate is a delivery problem, a rising decline rate is a
	// pricing one.
	RejectTimeout ReserveRejection = "timeout"
)

// OfferRepliedPayload is a driver's answer to a dispatched offer.
type OfferRepliedPayload struct {
	TripID   string `json:"tripId"`
	DriverID string `json:"driverId"`
	Accepted bool   `json:"accepted"`
}

// DecodeGeoEvent parses an envelope and checks that the payload matching its
// tag is actually present.
//
// The check matters: a message whose tag and payload disagree would otherwise
// dereference a nil pointer deep inside a shard loop, taking down every cell
// that instance owns rather than dropping one bad record.
func DecodeGeoEvent(raw []byte) (GeoEvent, error) {
	var event GeoEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		return GeoEvent{}, fmt.Errorf("wire: decode geo event: %w", err)
	}

	present := map[string]bool{
		TagDriverMoved:       event.Moved != nil,
		TagDriverEnteredCell: event.Entered != nil,
		TagDriverLeftCell:    event.Left != nil,
		TagMatchRequested:    event.Requested != nil,
		TagReserveDriver:     event.Reserve != nil,
		TagReserveResult:     event.Reserved != nil,
		TagOfferReplied:      event.Replied != nil,
	}

	ok, known := present[event.Tag]
	if !known {
		return GeoEvent{}, fmt.Errorf("wire: unknown geo event tag %q", event.Tag)
	}
	if !ok {
		return GeoEvent{}, fmt.Errorf("wire: geo event tagged %s carries no matching payload", event.Tag)
	}
	if event.Cell == "" {
		return GeoEvent{}, fmt.Errorf("wire: geo event tagged %s carries no cell", event.Tag)
	}

	return event, nil
}

// TagOffer is a dispatched ride offer, carried on `ws.push` keyed by driver id.
const TagOffer = "Offer"

// Offer is what a driver is actually shown.
//
// It travels on a different topic from `geo.events` because it is addressed to
// a person rather than to a place — the gateway routes it by driver id to
// whichever connection that driver holds.
type Offer struct {
	Tag      string `json:"_tag"`
	TripID   string `json:"tripId"`
	DriverID string `json:"driverId"`
	RiderID  string `json:"riderId"`

	PickupLat float64 `json:"pickupLat"`
	PickupLng float64 `json:"pickupLng"`

	// ReplyCell is where the answer must be sent, and it is the cell of the
	// shard that made the reservation — NOT the driver's current cell.
	//
	// The distinction matters because a driver moves. By the time they tap
	// accept they may be in a different cell owned by a different instance, and
	// routing the reply by their current position would deliver it to a shard
	// holding no reservation for it. The reservation's owner is the only shard
	// that can resolve the offer, so the offer carries its return address.
	ReplyCell string `json:"replyCell"`

	ExpiresAtMs    int64 `json:"expiresAtMs"`
	DispatchedAtMs int64 `json:"dispatchedAtMs"`
	// RequestedAtMs is when the rider asked, carried the whole way through so
	// match latency is measured end to end rather than per hop.
	RequestedAtMs int64 `json:"requestedAtMs"`
}
