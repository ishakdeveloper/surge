// Package matcher is the geo-sharded matching engine.
//
// One Kafka partition, one goroutine, one Shard. No mutex appears anywhere in
// this file and that is the entire design: a driver belongs to exactly one
// resolution-7 cell, a cell belongs to exactly one partition, and a partition
// is consumed by exactly one instance. Two riders competing for one driver
// cannot race because both requests are serialised through the same loop.
//
// The classic answer to that contention is a distributed lock. This is the
// other answer: arrange for the shared state not to be shared.
package domain

import (
	"fmt"
	"sort"
	"time"

	"github.com/ishakdeveloper/surge/shared/geo"
	"github.com/ishakdeveloper/surge/shared/wire"
)

type Config struct {
	// SearchRings is how far to expand the resolution-9 ring before giving up.
	// 4 rings is roughly 1.5 km at this resolution.
	SearchRings int
	// MaxCandidates caps how many drivers a single request will try before it
	// is abandoned. Without it a request in an empty part of the city walks the
	// whole index.
	MaxCandidates int
	// OfferTTL is how long a driver has to answer before the reservation is
	// released and the trip moves on.
	OfferTTL time.Duration
	// RequestTTL abandons a request that has exhausted its candidates without
	// finding anyone.
	RequestTTL time.Duration
	// DepartedGrace is how long a driver who has left this shard is still
	// remembered as a candidate.
	//
	// This is what keeps the cross-shard reservation path live rather than
	// theoretical. A driver two seconds gone is still the nearest car; the only
	// thing that changed is which instance may reserve them. Remembering them
	// briefly means the request follows them across the boundary instead of
	// pretending they vanished.
	DepartedGrace time.Duration
	// IdempotencyWindow is how long a request key is remembered, so an
	// at-least-once redelivery cannot dispatch the same trip twice.
	IdempotencyWindow time.Duration

	// Strategy is how requests are matched: one at a time as they arrive, or
	// gathered over BatchWindow and solved together. See batch.go.
	Strategy Strategy
	// BatchWindow is how long a batched shard gathers requests before solving.
	// Long enough to see riders competing for the same cars, short enough that
	// nobody waiting for one notices.
	BatchWindow time.Duration
	// BatchMaxRequests and BatchMaxDrivers bound a single solve. The travel-time
	// matrix is drivers × requests, and the solver is cubic.
	BatchMaxRequests int
	BatchMaxDrivers  int
	// BatchTimeout gives up on travel times that never came back, and solves
	// that batch over straight-line distance instead.
	BatchTimeout time.Duration
}

func DefaultConfig() Config {
	return Config{
		SearchRings:       4,
		MaxCandidates:     5,
		OfferTTL:          8 * time.Second,
		RequestTTL:        45 * time.Second,
		DepartedGrace:     10 * time.Second,
		IdempotencyWindow: 5 * time.Minute,
		Strategy:          StrategyGreedy,
		BatchWindow:       2 * time.Second,
		BatchMaxRequests:  32,
		BatchMaxDrivers:   64,
		BatchTimeout:      5 * time.Second,
	}
}

type driverState struct {
	id        string
	point     geo.Point
	indexCell geo.Cell
	shardCell string
	status    wire.DriverStatus
	heading   float64
	epoch     uint64
	seq       uint64
	// reservedFor is the trip holding this driver, or "" when free. A single
	// string is the whole mutual exclusion mechanism, and it is safe because
	// only one goroutine ever touches it.
	reservedFor string
}

func (d *driverState) available() bool {
	return d.status.Available() && d.reservedFor == ""
}

// departedDriver is a driver who has left, kept briefly so a request can follow
// them across a shard boundary.
type departedDriver struct {
	toCell string
	point  geo.Point
	at     time.Time
}

type candidate struct {
	driverID string
	// ownedHere is true when this shard still holds the driver, which decides
	// whether the reservation is a local function call or a Kafka round trip.
	ownedHere bool
	// cell is where to address the reservation when it is not local.
	cell     string
	distance float64
}

type pendingRequest struct {
	payload    wire.MatchRequestPayload
	candidates []candidate
	next       int
	// outstanding is the driver currently being reserved, if any.
	outstanding string
	deadline    time.Time
}

type heldOffer struct {
	tripID    string
	driverID  string
	replyCell string
	// requestedAtMs travels with the offer so latency is measured from when the
	// rider asked, not from when this shard got to it.
	requestedAtMs int64
	expires       time.Time
}

// Outcome is what a shard produces from handling one event: messages to send,
// and facts worth counting.
type Outcome struct {
	// GeoEvents are addressed to a cell and go to `geo.events`.
	GeoEvents []wire.GeoEvent
	// Offers are addressed to a driver and go to `ws.push`.
	Offers []wire.Offer
	// Matched reports a completed match and how long it took end to end.
	Matched []MatchResult
	// Abandoned reports requests that found nobody.
	Abandoned []string
	// Rejections counts reservation refusals by reason — the contention signal.
	Rejections []wire.ReserveRejection
	// Dispatches are the offers sent, with where the car and the rider were.
	// Match latency is what a rider feels now; the drive to the pickup is what
	// they feel next, waiting on the kerb — and it is what batching exists to
	// shrink.
	Dispatches []Dispatch
	// Batch asks the runner for travel times, when a batched shard closes its
	// window. The answer comes back through Solved.
	Batch *BatchRequest
}

// Dispatch is one offer: which car, and how far it is from its rider.
type Dispatch struct {
	TripID   string
	DriverID string
	Driver   geo.Point
	Pickup   geo.Point
	// Meters is the straight-line distance. Road time needs a router, and is
	// priced afterwards from the positions (see `make bench-matching`).
	Meters float64
}

func dispatched(tripID, driverID string, driver, pickup geo.Point) Dispatch {
	return Dispatch{
		TripID: tripID, DriverID: driverID, Driver: driver, Pickup: pickup,
		Meters: geo.DistanceMeters(driver, pickup),
	}
}

type MatchResult struct {
	TripID   string
	DriverID string
	Latency  time.Duration
}

// Shard is one partition's world.
type Shard struct {
	partition int32
	config    Config

	drivers  map[string]*driverState
	cells    map[geo.Cell]map[string]struct{}
	departed map[string]departedDriver

	pending map[string]*pendingRequest
	offers  map[string]*heldOffer

	// seen is the idempotency window: request keys already acted on.
	seen map[string]time.Time

	// restoredHolds are reservations recovered from a checkpoint whose driver
	// has not been seen again yet, applied when they reappear.
	restoredHolds map[string]string
	// checkpointed tracks every cell this shard has written a checkpoint for,
	// so it can write an empty record to supersede it.
	checkpointed map[string]struct{}

	// Batched matching: requests waiting for the window to close, when it
	// opened, and the batch whose travel times are being fetched.
	waiting      []string
	windowOpened time.Time
	inflight     *batch
	batchSeq     uint64
}

func NewShard(partition int32, config Config) *Shard {
	return &Shard{
		partition: partition,
		config:    config,
		drivers:   make(map[string]*driverState),
		cells:     make(map[geo.Cell]map[string]struct{}),
		departed:  make(map[string]departedDriver),
		pending:   make(map[string]*pendingRequest),
		offers:    make(map[string]*heldOffer),
		seen:      make(map[string]time.Time),

		restoredHolds: make(map[string]string),
		checkpointed:  make(map[string]struct{}),
	}
}

func (s *Shard) Partition() int32 { return s.partition }
func (s *Shard) Drivers() int     { return len(s.drivers) }
func (s *Shard) Pending() int     { return len(s.pending) }
func (s *Shard) Offers() int      { return len(s.offers) }

// Handle processes one event and returns everything that follows from it.
//
// Pure with respect to the outside world: it mutates only this shard's own
// state and returns messages rather than sending them. That is what makes the
// matching logic testable without a broker, and it is why every test below is a
// function call rather than an integration.
func (s *Shard) Handle(event wire.GeoEvent, now time.Time) (Outcome, error) {
	switch event.Tag {
	case wire.TagDriverEnteredCell:
		return s.driverEntered(event, now)
	case wire.TagDriverLeftCell:
		return s.driverLeft(event, now)
	case wire.TagDriverMoved:
		return s.driverMoved(event)
	case wire.TagMatchRequested:
		return s.matchRequested(event, now)
	case wire.TagReserveDriver:
		return s.reserveDriver(event, now)
	case wire.TagReserveResult:
		return s.reserveResult(event, now)
	case wire.TagOfferReplied:
		return s.offerReplied(event, now)
	default:
		return Outcome{}, fmt.Errorf("matcher: unhandled event tag %q", event.Tag)
	}
}

func (s *Shard) driverEntered(event wire.GeoEvent, now time.Time) (Outcome, error) {
	payload := event.Entered

	indexCell, err := geo.IndexCell(geo.Point{Lat: payload.Lat, Lng: payload.Lng})
	if err != nil {
		return Outcome{}, err
	}

	// Superseded only within the same session; see wire.DriverPing on why a
	// sequence number without an epoch freezes a restarted client forever.
	if existing, ok := s.drivers[payload.DriverID]; ok && superseded(existing, payload.Epoch, payload.Seq) {
		// An entry older than what we hold. Happens because the two halves of a
		// handover travel on different partitions and can arrive reversed.
		return Outcome{}, nil
	}

	delete(s.departed, payload.DriverID)

	driver := &driverState{
		id:        payload.DriverID,
		point:     geo.Point{Lat: payload.Lat, Lng: payload.Lng},
		indexCell: indexCell,
		shardCell: event.Cell,
		status:    payload.Status,
		heading:   payload.Heading,
		epoch:     payload.Epoch,
		seq:       payload.Seq,
	}

	if trip, held := s.restoredHolds[payload.DriverID]; held {
		// A reservation that survived a rebalance, meeting its driver again.
		if _, live := s.offers[trip]; live {
			driver.reservedFor = trip
		}
		delete(s.restoredHolds, payload.DriverID)
	}

	s.place(driver)
	return Outcome{}, nil
}

func (s *Shard) driverLeft(event wire.GeoEvent, now time.Time) (Outcome, error) {
	payload := event.Left

	driver, ok := s.drivers[payload.DriverID]
	if !ok {
		return Outcome{}, nil
	}
	if payload.Epoch == driver.epoch && payload.Seq < driver.seq {
		// A leave older than the position we hold: the driver came back before
		// this message arrived. Dropping it is what stops the reversed pair
		// from deleting a driver who is present.
		return Outcome{}, nil
	}
	if payload.Epoch < driver.epoch {
		// A straggler from a session that has already ended.
		return Outcome{}, nil
	}

	// A reserved driver is not released by walking out of the cell. The
	// reservation is a promise to a rider, and it outlives the geography — the
	// offer's own deadline is what ends it.
	if driver.reservedFor != "" {
		return Outcome{}, nil
	}

	s.remove(driver)
	s.departed[payload.DriverID] = departedDriver{
		toCell: payload.To,
		point:  driver.point,
		at:     now,
	}

	return Outcome{}, nil
}

func (s *Shard) driverMoved(event wire.GeoEvent) (Outcome, error) {
	payload := event.Moved

	driver, ok := s.drivers[payload.DriverID]
	if !ok {
		// A move for a driver we have not been handed yet. Dropped rather than
		// invented: the entry event carries the full state and will arrive.
		return Outcome{}, nil
	}
	if superseded(driver, payload.Epoch, payload.Seq) {
		return Outcome{}, nil
	}

	indexCell, err := geo.IndexCell(geo.Point{Lat: payload.Lat, Lng: payload.Lng})
	if err != nil {
		return Outcome{}, err
	}

	if indexCell != driver.indexCell {
		s.unindex(driver)
		driver.indexCell = indexCell
		s.index(driver)
	}

	driver.point = geo.Point{Lat: payload.Lat, Lng: payload.Lng}
	driver.epoch = payload.Epoch
	driver.seq = payload.Seq
	driver.status = payload.Status
	driver.heading = payload.Heading

	return Outcome{}, nil
}

// superseded reports whether an update is older than what is already held.
//
// Ordering only holds inside an epoch. A newer epoch always wins — it is a new
// client session, and its counter starts again — and an older one never does.
func superseded(driver *driverState, epoch, seq uint64) bool {
	if epoch != driver.epoch {
		return epoch < driver.epoch
	}
	return seq <= driver.seq
}

func (s *Shard) matchRequested(event wire.GeoEvent, now time.Time) (Outcome, error) {
	payload := event.Requested

	// At-least-once delivery means this exact request can arrive twice — after
	// a rebalance, or a redelivery on a failed commit. Dispatching it twice
	// would send two drivers to one rider.
	if _, already := s.seen[payload.IdempotencyKey]; already {
		return Outcome{}, nil
	}
	s.seen[payload.IdempotencyKey] = now

	if _, active := s.pending[payload.TripID]; active {
		return Outcome{}, nil
	}

	if s.config.Strategy == StrategyBatched {
		s.pending[payload.TripID] = &pendingRequest{
			payload:  *payload,
			deadline: now.Add(s.config.RequestTTL),
		}
		s.queue(payload.TripID, now)
		return Outcome{}, nil
	}

	pickup := geo.Point{Lat: payload.PickupLat, Lng: payload.PickupLng}
	candidates, err := s.rank(pickup, now)
	if err != nil {
		return Outcome{}, err
	}

	if len(candidates) == 0 {
		return Outcome{Abandoned: []string{payload.TripID}}, nil
	}

	request := &pendingRequest{
		payload:    *payload,
		candidates: candidates,
		deadline:   now.Add(s.config.RequestTTL),
	}
	s.pending[payload.TripID] = request

	return s.advance(request, now), nil
}

// advance tries the next candidate for a request.
//
// The local case is a direct call rather than a message to ourselves. Routing
// every reservation through Kafka would be uniform and would also add a broker
// round trip to the overwhelmingly common case of a driver this shard already
// owns — and since we found the candidate in our own index, "do we own them" is
// already known.
func (s *Shard) advance(request *pendingRequest, now time.Time) Outcome {
	for request.next < len(request.candidates) {
		next := request.candidates[request.next]
		request.next++

		if !next.ownedHere {
			request.outstanding = next.driverID

			return Outcome{GeoEvents: []wire.GeoEvent{{
				Tag:  wire.TagReserveDriver,
				Cell: next.cell,
				AtMs: now.UnixMilli(),
				Reserve: &wire.ReservePayload{
					DriverID:   next.driverID,
					TripID:     request.payload.TripID,
					ReplyCell:  s.cellOf(request.payload),
					DeadlineMs: now.Add(s.config.OfferTTL).UnixMilli(),
					RiderID:    request.payload.RiderID,
					PickupLat:  request.payload.PickupLat,
					PickupLng:  request.payload.PickupLng,
				},
			}}}
		}

		driver, ok := s.drivers[next.driverID]
		if !ok || !driver.available() {
			// Taken since ranking — by another request in this very loop. This
			// is the contention case, and handling it is a `continue`.
			continue
		}

		driver.reservedFor = request.payload.TripID
		request.outstanding = driver.id

		offer := wire.Offer{
			Tag:            wire.TagOffer,
			TripID:         request.payload.TripID,
			DriverID:       driver.id,
			RiderID:        request.payload.RiderID,
			PickupLat:      request.payload.PickupLat,
			PickupLng:      request.payload.PickupLng,
			ReplyCell:      driver.shardCell,
			ExpiresAtMs:    now.Add(s.config.OfferTTL).UnixMilli(),
			DispatchedAtMs: now.UnixMilli(),
			RequestedAtMs:  request.payload.RequestedAtMs,
		}

		s.offers[request.payload.TripID] = &heldOffer{
			tripID:        request.payload.TripID,
			driverID:      driver.id,
			replyCell:     s.cellOf(request.payload),
			requestedAtMs: request.payload.RequestedAtMs,
			expires:       now.Add(s.config.OfferTTL),
		}

		pickup := geo.Point{Lat: request.payload.PickupLat, Lng: request.payload.PickupLng}
		return Outcome{
			Offers:     []wire.Offer{offer},
			Dispatches: []Dispatch{dispatched(request.payload.TripID, driver.id, driver.point, pickup)},
		}
	}

	delete(s.pending, request.payload.TripID)
	return Outcome{Abandoned: []string{request.payload.TripID}}
}

// reserveDriver is the cross-shard half: another shard asking us to hold one of
// our drivers. Serialised through this loop, which is why no lock is needed.
func (s *Shard) reserveDriver(event wire.GeoEvent, now time.Time) (Outcome, error) {
	payload := event.Reserve

	reject := func(reason wire.ReserveRejection) Outcome {
		return Outcome{
			Rejections: []wire.ReserveRejection{reason},
			GeoEvents: []wire.GeoEvent{{
				Tag:  wire.TagReserveResult,
				Cell: payload.ReplyCell,
				AtMs: now.UnixMilli(),
				Reserved: &wire.ReserveResultPayload{
					DriverID: payload.DriverID,
					TripID:   payload.TripID,
					OK:       false,
					Reason:   reason,
				},
			}},
		}
	}

	driver, ok := s.drivers[payload.DriverID]
	switch {
	case !ok:
		return reject(wire.RejectUnknownDriver), nil
	case driver.reservedFor != "":
		return reject(wire.RejectBusy), nil
	case !driver.status.Available():
		return reject(wire.RejectUnavailable), nil
	}

	driver.reservedFor = payload.TripID

	s.offers[payload.TripID] = &heldOffer{
		tripID:        payload.TripID,
		driverID:      driver.id,
		replyCell:     payload.ReplyCell,
		requestedAtMs: 0,
		expires:       time.UnixMilli(payload.DeadlineMs),
	}

	dispatch := dispatched(payload.TripID, driver.id, driver.point, geo.Point{Lat: payload.PickupLat, Lng: payload.PickupLng})

	return Outcome{Offers: []wire.Offer{{
		Tag:            wire.TagOffer,
		TripID:         payload.TripID,
		DriverID:       driver.id,
		RiderID:        payload.RiderID,
		PickupLat:      payload.PickupLat,
		PickupLng:      payload.PickupLng,
		ReplyCell:      driver.shardCell,
		ExpiresAtMs:    payload.DeadlineMs,
		DispatchedAtMs: now.UnixMilli(),
	}}, Dispatches: []Dispatch{dispatch}}, nil
}

// reserveResult is the requesting shard learning how a cross-shard reservation
// went. Success ends the request; failure moves to the next candidate.
func (s *Shard) reserveResult(event wire.GeoEvent, now time.Time) (Outcome, error) {
	payload := event.Reserved

	request, ok := s.pending[payload.TripID]
	if !ok {
		return Outcome{}, nil
	}

	if payload.OK {
		delete(s.pending, payload.TripID)
		return Outcome{Matched: []MatchResult{{
			TripID:   payload.TripID,
			DriverID: payload.DriverID,
			Latency:  now.Sub(time.UnixMilli(request.payload.RequestedAtMs)),
		}}}, nil
	}

	request.outstanding = ""
	outcome := s.advance(request, now)
	outcome.Rejections = append(outcome.Rejections, payload.Reason)

	return outcome, nil
}

// offerReplied resolves an offer this shard is holding.
func (s *Shard) offerReplied(event wire.GeoEvent, now time.Time) (Outcome, error) {
	payload := event.Replied

	offer, ok := s.offers[payload.TripID]
	if !ok {
		// Already expired, or answered twice. Neither is an error: an offer
		// that timed out and is then accepted must not resurrect, or the rider
		// gets a driver the system already gave away.
		return Outcome{}, nil
	}
	delete(s.offers, payload.TripID)

	if driver, held := s.drivers[offer.driverID]; held && driver.reservedFor == payload.TripID {
		if payload.Accepted {
			driver.status = wire.StatusEnRoutePickup
		}
		driver.reservedFor = ""
	}

	// The reply belongs to whoever asked. When that is this shard, resolve it
	// here rather than round-tripping a message to ourselves.
	if request, mine := s.pending[payload.TripID]; mine {
		if payload.Accepted {
			delete(s.pending, payload.TripID)
			return Outcome{Matched: []MatchResult{{
				TripID:   payload.TripID,
				DriverID: payload.DriverID,
				Latency:  now.Sub(time.UnixMilli(request.payload.RequestedAtMs)),
			}}}, nil
		}

		request.outstanding = ""
		outcome := s.advance(request, now)
		outcome.Rejections = append(outcome.Rejections, wire.RejectDeclined)
		return outcome, nil
	}

	result := &wire.ReserveResultPayload{
		DriverID: payload.DriverID,
		TripID:   payload.TripID,
		OK:       payload.Accepted,
	}
	if !payload.Accepted {
		result.Reason = wire.RejectDeclined
	}

	return Outcome{GeoEvents: []wire.GeoEvent{{
		Tag:      wire.TagReserveResult,
		Cell:     offer.replyCell,
		AtMs:     now.UnixMilli(),
		Reserved: result,
	}}}, nil
}

// Tick expires offers and requests, and evicts stale bookkeeping.
//
// Every hold in this system has a deadline, because the alternative is a driver
// stranded by a rider who closed their laptop, or a reservation orphaned by an
// instance that died mid-offer. Expiry is what makes at-least-once delivery and
// abrupt process death survivable without any distributed coordination.
func (s *Shard) Tick(now time.Time) Outcome {
	var outcome Outcome

	for tripID, offer := range s.offers {
		if now.Before(offer.expires) {
			continue
		}
		delete(s.offers, tripID)

		if driver, ok := s.drivers[offer.driverID]; ok && driver.reservedFor == tripID {
			driver.reservedFor = ""
		}

		if request, mine := s.pending[tripID]; mine {
			request.outstanding = ""
			outcome.merge(s.advance(request, now))
		} else {
			outcome.GeoEvents = append(outcome.GeoEvents, wire.GeoEvent{
				Tag:  wire.TagReserveResult,
				Cell: offer.replyCell,
				AtMs: now.UnixMilli(),
				Reserved: &wire.ReserveResultPayload{
					DriverID: offer.driverID,
					TripID:   tripID,
					OK:       false,
					Reason:   wire.RejectTimeout,
				},
			})
		}

		outcome.Rejections = append(outcome.Rejections, wire.RejectTimeout)
	}

	for tripID, request := range s.pending {
		if now.After(request.deadline) {
			delete(s.pending, tripID)
			outcome.Abandoned = append(outcome.Abandoned, tripID)
		}
	}

	for id, departed := range s.departed {
		if now.Sub(departed.at) > s.config.DepartedGrace {
			delete(s.departed, id)
		}
	}

	for key, at := range s.seen {
		if now.Sub(at) > s.config.IdempotencyWindow {
			delete(s.seen, key)
		}
	}

	for driverID, trip := range s.restoredHolds {
		if _, live := s.offers[trip]; !live {
			delete(s.restoredHolds, driverID)
		}
	}

	if s.config.Strategy == StrategyBatched {
		outcome.merge(s.maybeBatch(now))
	}

	return outcome
}

// rank finds nearby drivers, nearest first.
func (s *Shard) rank(pickup geo.Point, now time.Time) ([]candidate, error) {
	origin, err := geo.IndexCell(pickup)
	if err != nil {
		return nil, err
	}

	ring, err := geo.Ring(origin, s.config.SearchRings)
	if err != nil {
		return nil, err
	}

	var found []candidate
	for _, cell := range ring {
		for id := range s.cells[cell] {
			driver, ok := s.drivers[id]
			if !ok || !driver.available() {
				continue
			}
			found = append(found, candidate{
				driverID:  id,
				ownedHere: true,
				cell:      driver.shardCell,
				distance:  geo.DistanceMeters(pickup, driver.point),
			})
		}
	}

	// Drivers who have just crossed out of this shard are still nearby, and
	// following them across the boundary is what exercises the cross-shard
	// reservation path in normal operation rather than only under a rebalance.
	for id, departed := range s.departed {
		if now.Sub(departed.at) > s.config.DepartedGrace {
			continue
		}
		distance := geo.DistanceMeters(pickup, departed.point)
		// Only worth chasing if they were plausibly close to begin with.
		if distance > 2000 {
			continue
		}
		found = append(found, candidate{
			driverID:  id,
			ownedHere: false,
			cell:      departed.toCell,
			distance:  distance,
		})
	}

	sort.Slice(found, func(a, b int) bool { return found[a].distance < found[b].distance })

	if len(found) > s.config.MaxCandidates {
		found = found[:s.config.MaxCandidates]
	}
	return found, nil
}

func (s *Shard) cellOf(payload wire.MatchRequestPayload) string {
	cell, err := geo.ShardCell(geo.Point{Lat: payload.PickupLat, Lng: payload.PickupLng})
	if err != nil {
		return ""
	}
	return cell.String()
}

func (s *Shard) place(driver *driverState) {
	s.drivers[driver.id] = driver
	s.index(driver)
}

func (s *Shard) index(driver *driverState) {
	bucket, ok := s.cells[driver.indexCell]
	if !ok {
		bucket = make(map[string]struct{})
		s.cells[driver.indexCell] = bucket
	}
	bucket[driver.id] = struct{}{}
}

func (s *Shard) unindex(driver *driverState) {
	if bucket, ok := s.cells[driver.indexCell]; ok {
		delete(bucket, driver.id)
		if len(bucket) == 0 {
			delete(s.cells, driver.indexCell)
		}
	}
}

func (s *Shard) remove(driver *driverState) {
	s.unindex(driver)
	delete(s.drivers, driver.id)
}

// Checkpoint captures the state that cannot be rebuilt from the stream.
//
// One record per cell this shard holds offers for, plus an empty record for
// every cell it has previously written — compaction keeps the last record per
// key, so an empty one is how a cell is told it has nothing outstanding.
func (s *Shard) Checkpoint(now time.Time) []wire.ShardCheckpoint {
	byCell := make(map[string][]wire.CheckpointedOffer)

	// Every cell that was ever written needs a record, or a stale non-empty one
	// survives compaction and a restoring shard reinstates offers that were
	// resolved before the handover.
	for cell := range s.checkpointed {
		byCell[cell] = nil
	}

	for _, offer := range s.offers {
		cell := offer.replyCell
		if driver, ok := s.drivers[offer.driverID]; ok {
			cell = driver.shardCell
		}

		byCell[cell] = append(byCell[cell], wire.CheckpointedOffer{
			TripID:        offer.tripID,
			DriverID:      offer.driverID,
			ReplyCell:     offer.replyCell,
			RequestedAtMs: offer.requestedAtMs,
			ExpiresAtMs:   offer.expires.UnixMilli(),
		})
	}

	records := make([]wire.ShardCheckpoint, 0, len(byCell))
	for cell, offers := range byCell {
		records = append(records, wire.ShardCheckpoint{
			Tag:    wire.TagShardCheckpoint,
			Cell:   cell,
			AtMs:   now.UnixMilli(),
			Offers: offers,
		})
		s.checkpointed[cell] = struct{}{}
	}

	return records
}

// Restore rebuilds the holds from a checkpoint.
//
// The drivers themselves are NOT restored, and cannot be: their positions come
// from the ping stream and will arrive on their own within a ping interval. So
// a restored hold is recorded against a driver the shard has not met yet, and
// applied the moment they turn up. In the meantime the offer's deadline still
// runs, which means a hold whose driver never reappears expires by itself
// rather than leaking.
func (s *Shard) Restore(records []wire.ShardCheckpoint, now time.Time) {
	for _, record := range records {
		s.checkpointed[record.Cell] = struct{}{}

		for _, offer := range record.Offers {
			expires := time.UnixMilli(offer.ExpiresAtMs)
			if !expires.After(now) {
				// Expired while the partition was in flight. Letting it lapse
				// is correct: the requester has already been told, or is about
				// to time out on its own.
				continue
			}

			s.offers[offer.TripID] = &heldOffer{
				tripID:        offer.TripID,
				driverID:      offer.DriverID,
				replyCell:     offer.ReplyCell,
				requestedAtMs: offer.RequestedAtMs,
				expires:       expires,
			}
			s.restoredHolds[offer.DriverID] = offer.TripID
		}
	}
}

// RestoredHolds is how many reservations are waiting for their driver to
// reappear. It falls to zero within a ping interval of a rebalance, and a value
// that stays above zero means drivers are not coming back.
func (s *Shard) RestoredHolds() int { return len(s.restoredHolds) }

// Frame is this shard's fleet, for the console: every driver it owns, and how
// many promises it is holding. Pure, like Handle — it reads and allocates, and
// the runner decides what to do with the result.
func (s *Shard) Frame() (drivers []wire.FleetDriver, pending, offers int) {
	drivers = make([]wire.FleetDriver, 0, len(s.drivers))
	for _, driver := range s.drivers {
		drivers = append(drivers, wire.FleetDriver{
			ID:       driver.id,
			Lat:      driver.point.Lat,
			Lng:      driver.point.Lng,
			Heading:  driver.heading,
			Status:   driver.status,
			Cell:     driver.shardCell,
			Reserved: driver.reservedFor != "",
		})
	}
	return drivers, len(s.pending), len(s.offers)
}
