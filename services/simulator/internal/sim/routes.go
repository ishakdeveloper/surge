package sim

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/ishakdeveloper/surge/shared/geo"
	"github.com/ishakdeveloper/surge/shared/routing"
)

// RoutePool is a shared corpus of real road routes.
//
// This is what lets driver count and Valhalla load be independent variables.
// The naive simulator asks the router for a fresh route every time a driver
// reaches its destination, which ties request rate to fleet size: 40,000
// drivers finishing a 15-minute trip is 44 route requests a second forever, and
// 40,000 of them at once on startup.
//
// Instead a few hundred routes are generated once and shared. Each driver picks
// one at random and starts at a random offset along it, so two drivers on the
// same route are in different places moving at different speeds. On a map it is
// indistinguishable from per-driver routing, and the router sees a constant
// trickle rather than a fleet-sized spike.
//
// The pool keeps refreshing in the background, so a long run does not replay
// the same few hundred paths forever.
type RoutePool struct {
	router *routing.Client
	bounds geo.Bounds

	mu     sync.RWMutex
	routes []*routing.Route

	// target is how many routes to hold. A few hundred is plenty: what matters
	// is that drivers are spread over the road network, not that every driver
	// has a private path.
	target int

	onFetch func(duration time.Duration, err error)
	onSize  func(size int)
}

func NewRoutePool(router *routing.Client, bounds geo.Bounds, target int) *RoutePool {
	return &RoutePool{router: router, bounds: bounds, target: target}
}

// OnFetch and OnSize let the daemon record metrics without this package
// importing Prometheus.
func (p *RoutePool) OnFetch(f func(time.Duration, error)) { p.onFetch = f }
func (p *RoutePool) OnSize(f func(int))                   { p.onSize = f }

// Fill blocks until the pool holds at least `min` routes, generating with
// `concurrency` workers. Returns early if ctx is cancelled.
//
// Bounded concurrency matters: Valhalla is single-instance and a few threads
// deep, so 200 parallel requests do not make it faster, they make every request
// slower and some of them time out.
func (p *RoutePool) Fill(ctx context.Context, min, concurrency int) error {
	if concurrency < 1 {
		concurrency = 1
	}

	var wg sync.WaitGroup
	for worker := range concurrency {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			source := rand.New(rand.NewPCG(uint64(seed)+1, 0x5eed))

			for {
				if ctx.Err() != nil {
					return
				}
				if p.Size() >= min {
					return
				}
				p.generateOne(ctx, source)
			}
		}(worker)
	}
	wg.Wait()

	if ctx.Err() != nil {
		return ctx.Err()
	}
	if p.Size() == 0 {
		return errors.New("sim: route pool is empty; is Valhalla serving tiles?")
	}
	return nil
}

// Run tops the pool up and slowly rotates it, so a long simulation does not
// replay the same corpus indefinitely.
func (p *RoutePool) Run(ctx context.Context, seed uint64) {
	source := rand.New(rand.NewPCG(seed, 0x1234abcd))
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.generateOne(ctx, source)
		}
	}
}

func (p *RoutePool) generateOne(ctx context.Context, source *rand.Rand) {
	from := p.bounds.Random(source)
	to := p.bounds.Random(source)

	// Reject trips that are trivially short before paying for a route: two
	// points 80 metres apart produce a path a driver finishes in one tick.
	if geo.DistanceMeters(from, to) < 1500 {
		return
	}

	started := time.Now()
	route, err := p.router.Route(ctx, from, to)
	if p.onFetch != nil {
		p.onFetch(time.Since(started), err)
	}

	if err != nil {
		// A NoRouteError is the expected case, not a problem: the bounding box
		// includes the IJ and the IJmeer, and points landing in water simply do
		// not route. Anything else is worth surfacing, but not worth stopping
		// the simulator over.
		return
	}

	p.mu.Lock()
	if len(p.routes) < p.target {
		p.routes = append(p.routes, route)
	} else {
		// Replace at random rather than FIFO, so the corpus turns over evenly
		// instead of cycling.
		p.routes[source.IntN(len(p.routes))] = route
	}
	size := len(p.routes)
	p.mu.Unlock()

	if p.onSize != nil {
		p.onSize(size)
	}
}

func (p *RoutePool) Size() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.routes)
}

// Random returns a route from the pool, or nil while it is still empty.
func (p *RoutePool) Random(source *rand.Rand) *routing.Route {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.routes) == 0 {
		return nil
	}
	return p.routes[source.IntN(len(p.routes))]
}

// RandomPoint returns a point somewhere along a random route.
//
// Used for rider pickups. Better than a random point in the bounding box for
// the same reason it is better for drivers: it is on a road. A pickup in the
// middle of the IJ produces a request no driver can reach, which would show up
// as a matching failure rather than as the bad input it is.
func (p *RoutePool) RandomPoint(source *rand.Rand) (geo.Point, bool) {
	route := p.Random(source)
	if route == nil {
		return geo.Point{}, false
	}

	point, _ := route.Path.At(source.Float64() * route.Path.Length())
	return point, true
}
