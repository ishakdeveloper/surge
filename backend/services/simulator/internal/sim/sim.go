// Package sim is the driver simulator: the load generator this whole project is
// built around.
//
// It exists before the product on purpose. Without it the system gets tested
// with three browser tabs and never meets a single interesting failure mode;
// with it there is a knob to turn until something breaks, and every graph
// produced afterwards is measurement rather than assertion.
package sim

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ishakdeveloper/surge/shared/geo"
	"github.com/ishakdeveloper/surge/shared/wire"
)

// Config is the knob.
type Config struct {
	// Drivers is fleet size. The headline number.
	Drivers int
	// PingInterval is how often each driver reports.
	//
	// Ingest rate is Drivers / PingInterval, and it is worth being explicit
	// that these are two ways to reach the same load: 10,000 drivers at 4s is
	// 2,500 pings/sec, not 10,000. Reaching 10k/sec means 40,000 drivers at 4s,
	// or 10,000 at 1s — and those two stress different things, since the first
	// multiplies index size and the second multiplies message rate.
	PingInterval time.Duration
	// Seed makes a run reproducible, which is what makes two runs comparable.
	Seed uint64

	// SpeedKmhMin and SpeedKmhMax bound the sampled cruising speed.
	SpeedKmhMin float64
	SpeedKmhMax float64

	// Epoch identifies this simulator run, stamped on every ping.
	//
	// Restarting the simulator is exactly a fleet of clients reinstalling at
	// once: sequence numbers go back to one. Without an epoch the consumer
	// rejects every one of them as a reordering and the whole fleet freezes at
	// its last known position — which is precisely what happened, and is why
	// this field exists.
	Epoch uint64

	// GPSNoiseMeters perturbs each reported position.
	//
	// Not cosmetic. Real GPS jitters, and a driver parked near a cell boundary
	// therefore flaps between two cells — which means repeated
	// DriverLeftCell/DriverEnteredCell pairs across two Kafka partitions, which
	// is exactly the case the per-driver sequence number exists to survive.
	// Setting this to zero would hide a class of bug the real system has.
	GPSNoiseMeters float64

	// TripKm turns trips on. A driver who accepts drives to the pickup, then
	// carries the rider about this far before cruising again, reporting
	// enroute_pickup and on_trip as it goes. Zero keeps the fleet the earlier
	// benchmarks measured, where accepting changes nothing and supply never
	// drains — which is also why those runs had no pickups to learn ETAs from.
	TripKm float64
}

func DefaultConfig() Config {
	return Config{
		Epoch:          uint64(time.Now().UnixNano()),
		Drivers:        1000,
		PingInterval:   4 * time.Second,
		Seed:           1,
		SpeedKmhMin:    18,
		SpeedKmhMax:    52,
		GPSNoiseMeters: 6,
	}
}

func (c Config) validate() error {
	switch {
	case c.Drivers < 0:
		return fmt.Errorf("sim: drivers must not be negative, got %d", c.Drivers)
	case c.PingInterval < 100*time.Millisecond:
		return fmt.Errorf("sim: ping interval %v is below the 100ms floor", c.PingInterval)
	case c.SpeedKmhMin <= 0 || c.SpeedKmhMax < c.SpeedKmhMin:
		return fmt.Errorf("sim: speed range %.1f-%.1f km/h is not usable", c.SpeedKmhMin, c.SpeedKmhMax)
	case c.GPSNoiseMeters < 0:
		return fmt.Errorf("sim: gps noise must not be negative")
	case c.TripKm < 0:
		return fmt.Errorf("sim: trip length must not be negative")
	}
	return nil
}

// Hooks are the observability seams, so this package does not import Prometheus.
type Hooks struct {
	OnPing      func()
	OnPingError func(error)
	OnArrival   func()
	OnDrivers   func(int)
}

// Sim runs a fleet.
type Sim struct {
	pool      *RoutePool
	transport Transport
	hooks     Hooks

	mu      sync.Mutex
	config  Config
	cancels []context.CancelFunc
	wg      sync.WaitGroup
	running atomic.Int64
	pings   atomic.Uint64

	// inboxes is where accepted trips reach their drivers: driver id to a
	// channel the driver's goroutine reads.
	inboxes sync.Map
}

func New(pool *RoutePool, transport Transport, config Config, hooks Hooks) (*Sim, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}
	return &Sim{pool: pool, transport: transport, config: config, hooks: hooks}, nil
}

// Config returns the current settings.
func (s *Sim) Config() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.config
}

// Running is the live driver count.
func (s *Sim) Running() int { return int(s.running.Load()) }

// Pings is the total emitted since start.
func (s *Sim) Pings() uint64 { return s.pings.Load() }

// Scale changes the fleet without restarting.
//
// Growing starts new drivers; shrinking cancels the most recently started ones.
// This is what makes the demo work: dragging 500 to 50,000 and watching p99
// move is a different experience from restarting a process and waiting.
//
// Settings other than Drivers apply to newly started drivers only. Existing
// ones keep the interval they were born with, because retuning a live ticker
// mid-flight would make the ramp itself a variable in the measurement.
func (s *Sim) Scale(ctx context.Context, config Config) error {
	if err := config.validate(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.config = config
	current := len(s.cancels)

	switch {
	case config.Drivers > current:
		for index := current; index < config.Drivers; index++ {
			driverCtx, cancel := context.WithCancel(ctx)
			s.cancels = append(s.cancels, cancel)
			s.wg.Add(1)
			go s.drive(driverCtx, index, config)
		}
	case config.Drivers < current:
		for _, cancel := range s.cancels[config.Drivers:] {
			cancel()
		}
		s.cancels = s.cancels[:config.Drivers]
	}

	if s.hooks.OnDrivers != nil {
		s.hooks.OnDrivers(len(s.cancels))
	}
	return nil
}

// Stop cancels every driver and waits for them to finish.
func (s *Sim) Stop() {
	s.mu.Lock()
	for _, cancel := range s.cancels {
		cancel()
	}
	s.cancels = nil
	s.mu.Unlock()

	s.wg.Wait()
}

// drive is one driver's whole life.
//
// A driver is not a position — it is a route plus a distance along it, advanced
// by speed x elapsed on every tick. That keeps per-tick work to a binary search
// over precomputed cumulative distances rather than a walk from the route's
// start, which is the difference between this scaling and not.
func (s *Sim) drive(ctx context.Context, index int, config Config) {
	defer s.wg.Done()

	// Seeded from the run seed and the driver index, so the same seed produces
	// the same fleet behaviour — which is what lets a greedy run and a batched
	// run be compared rather than merely contrasted.
	source := rand.New(rand.NewPCG(config.Seed, uint64(index)))
	id := fmt.Sprintf("drv-%06d", index)

	// Spread the fleet across the interval before doing anything. Without this
	// every driver reports on the same tick and the broker sees a fleet-sized
	// spike four times a minute instead of a steady rate — which would measure
	// burst handling while claiming to measure throughput.
	phase := time.Duration(source.Float64() * float64(config.PingInterval))
	select {
	case <-ctx.Done():
		return
	case <-time.After(phase):
	}

	s.running.Add(1)
	defer s.running.Add(-1)

	// Accepted trips arrive here, from whichever transport answered the offer.
	inbox := make(chan wire.Offer, 1)
	s.inboxes.Store(id, inbox)
	defer s.inboxes.Delete(id)

	start := s.pool.Random(source)
	if start == nil {
		return
	}
	var (
		path = start.Path
		// Start somewhere along the route rather than at its head, so drivers
		// sharing a route are scattered along it instead of convoying.
		distance = source.Float64() * start.Path.Length()
		seq      uint64
		status   = wire.StatusIdle
	)

	speedMps := (config.SpeedKmhMin + source.Float64()*(config.SpeedKmhMax-config.SpeedKmhMin)) / 3.6

	ticker := time.NewTicker(config.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case offer := <-inbox:
			// A trip, if trips are on and this driver is free to take one. The
			// leg to the pickup is a real route, so the time it takes is the
			// time a pickup takes — which is what ETA prediction learns from.
			if config.TripKm <= 0 || status != wire.StatusIdle {
				continue
			}
			from, _ := path.At(distance)
			path, distance = s.leg(ctx, from, geo.Point{Lat: offer.PickupLat, Lng: offer.PickupLng}), 0
			status = wire.StatusEnRoutePickup
			continue
		case <-ticker.C:
		}

		distance += speedMps * config.PingInterval.Seconds()

		if distance >= path.Length() {
			switch status {
			case wire.StatusEnRoutePickup:
				// At the pickup. Carry the rider somewhere about TripKm away.
				pickup, _ := path.At(path.Length())
				path, distance = s.leg(ctx, pickup, s.destination(source, pickup, config.TripKm*1000)), 0
				status = wire.StatusOnTrip
			default:
				// Arrived, or dropped the rider off. Pick a new route and carry on
				// cruising — an idle driver in a city does not park, it circles.
				status = wire.StatusIdle
				if next := s.pool.Random(source); next != nil {
					path = next.Path
					distance = 0
					speedMps = (config.SpeedKmhMin + source.Float64()*(config.SpeedKmhMax-config.SpeedKmhMin)) / 3.6
				} else {
					distance = path.Length()
				}
				if s.hooks.OnArrival != nil {
					s.hooks.OnArrival()
				}
			}
		}

		position, heading := path.At(distance)
		position = jitter(position, config.GPSNoiseMeters, source)

		seq++
		ping := wire.DriverPing{
			DriverID: id,
			Epoch:    config.Epoch,
			Seq:      seq,
			Lat:      position.Lat,
			Lng:      position.Lng,
			Heading:  heading,
			SpeedMps: speedMps,
			Status:   status,
			SentAtMs: time.Now().UnixMilli(),
		}

		if err := s.transport.Ping(ctx, ping); err != nil {
			if s.hooks.OnPingError != nil {
				s.hooks.OnPingError(err)
			}
			continue
		}

		s.pings.Add(1)
		if s.hooks.OnPing != nil {
			s.hooks.OnPing()
		}
	}
}

// Assign hands an accepted trip to its driver. Non-blocking: a driver already
// on a trip ignores a second one, as a real driver would.
func (s *Sim) Assign(offer wire.Offer) {
	inbox, ok := s.inboxes.Load(offer.DriverID)
	if !ok {
		return
	}
	select {
	case inbox.(chan wire.Offer) <- offer:
	default:
	}
}

// leg is a drivable path between two points: Valhalla's, or a straight line
// when it has none, because a simulated driver must never be stranded by a
// gap in the road network.
func (s *Sim) leg(ctx context.Context, from, to geo.Point) *geo.Path {
	if s.pool.router != nil {
		routeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		route, err := s.pool.router.Route(routeCtx, from, to)
		cancel()
		if err == nil {
			return route.Path
		}
	}
	if path, err := geo.NewPath([]geo.Point{from, to}); err == nil {
		return path
	}
	// Already there: a path a metre long, so the next tick arrives.
	path, _ := geo.NewPath([]geo.Point{from, {Lat: from.Lat + 1e-5, Lng: from.Lng}})
	return path
}

// destination is where a trip ends: of many points on the road network, the
// one whose distance from the pickup is closest to the target. Drawn from the
// driver's own seeded source, so a run is repeatable.
//
// Many, not a few: the ring 1-2 km around a pickup is a few per cent of the
// city, and eight tries missed it most of the time, sending trips far longer
// or shorter than asked. Each try is a lookup in memory, not a route.
func (s *Sim) destination(source *rand.Rand, pickup geo.Point, meters float64) geo.Point {
	best, gap := pickup, math.Inf(1)
	for range 64 {
		point, ok := s.pool.RandomPoint(source)
		if !ok {
			continue
		}
		if off := math.Abs(geo.DistanceMeters(pickup, point) - meters); off < gap {
			best, gap = point, off
		}
	}
	return best
}

// jitter displaces a point by a random offset with the given standard
// deviation in metres.
func jitter(p geo.Point, meters float64, source *rand.Rand) geo.Point {
	if meters <= 0 {
		return p
	}

	// Metres to degrees. Longitude compresses with latitude; at Amsterdam's
	// 52.4° a degree of longitude is about 68 km against 111 km of latitude,
	// and ignoring that would make the noise visibly elliptical.
	const metersPerDegreeLat = 111_320.0
	metersPerDegreeLng := metersPerDegreeLat * math.Cos(p.Lat*math.Pi/180)

	return geo.Point{
		Lat: p.Lat + source.NormFloat64()*meters/metersPerDegreeLat,
		Lng: p.Lng + source.NormFloat64()*meters/metersPerDegreeLng,
	}
}
