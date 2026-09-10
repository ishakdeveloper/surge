package sim

import (
	"math/rand/v2"
	"sync"
	"time"

	"github.com/ishakdeveloper/surge/shared/wire"
)

// OfferPolicy is how a simulated driver decides.
//
// Extracted from the Kafka path so the WebSocket path behaves identically. The
// point of moving the fleet onto real sockets is to change the transport and
// nothing else — if the drivers also started answering differently, any change
// in the numbers would be unattributable.
type OfferPolicy struct {
	acceptRate    float64
	acceptLatency time.Duration

	mu     sync.Mutex
	source *rand.Rand
	// accepted is the double-dispatch detector: which driver holds which trip.
	//
	// A trip may legitimately be offered to several drivers in turn, which is
	// what a decline produces. What must never happen is two drivers holding
	// live offers for the same trip, because both could accept.
	accepted map[string]string
}

func NewOfferPolicy(config RiderConfig) *OfferPolicy {
	return &OfferPolicy{
		acceptRate:    config.AcceptRate,
		acceptLatency: config.AcceptLatency,
		source:        rand.New(rand.NewPCG(config.Seed, 0xd21e5)),
		accepted:      make(map[string]string),
	}
}

// Decision is what a driver does with an offer.
type Decision struct {
	Accept bool
	// Delay is how long they take to answer. The reservation is held the whole
	// time, so this is not cosmetic: it is the dominant term in match latency
	// and the reason offers need deadlines.
	Delay time.Duration
	// Duplicate means this trip was already accepted by someone else — the
	// failure the sharding exists to prevent.
	Duplicate bool
}

// Decide answers an offer.
func (p *OfferPolicy) Decide(offer wire.Offer) Decision {
	p.mu.Lock()
	defer p.mu.Unlock()

	if holder, taken := p.accepted[offer.TripID]; taken && holder != offer.DriverID {
		// A real driver app would refuse, and so does this — but the fact that
		// it was offered at all is what gets counted.
		return Decision{Accept: false, Duplicate: true}
	}

	accept := p.source.Float64() < p.acceptRate
	// Jittered, because a fleet that all answers at exactly 1500ms would make
	// the offer deadline a cliff rather than a distribution.
	delay := time.Duration(float64(p.acceptLatency) * (0.5 + p.source.Float64()))

	return Decision{Accept: accept, Delay: delay}
}

// Commit records an acceptance, or reports that somebody beat us to it.
//
// Separate from Decide because the delay happens in between: a driver thinks
// for a second and a half, and in that time another driver may have taken the
// same trip. Checking only at decision time would miss exactly the race worth
// detecting.
func (p *OfferPolicy) Commit(offer wire.Offer) (ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if holder, taken := p.accepted[offer.TripID]; taken && holder != offer.DriverID {
		return false
	}
	p.accepted[offer.TripID] = offer.DriverID
	return true
}
