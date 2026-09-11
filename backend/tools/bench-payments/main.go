// Command bench-payments measures what holding the fare adds between a booking
// and its dispatch.
//
// With TRIP_REQUIRE_PAYMENT off, trip asks the matcher for a driver inside the
// booking call. With it on, the booking waits in payment_pending while three
// hops happen: trip's outbox relays TripRequested, payments places the hold and
// its outbox relays PaymentAuthorized, and trip consumes that and asks the
// matcher. This books real trips through trip's gRPC and times every hop from
// the Kafka record each one leaves behind, so the answer is per stage rather
// than one number nobody can act on.
//
// Dispatch is the MatchRequested trip writes to geo.events. No matcher and no
// fleet are needed: payments sits in front of the matcher, not inside it.
//
// Every trip it books is cancelled at the end, which releases the hold. Run it
// from scripts/bench-payments.sh, which restarts trip in each mode.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"os"
	"os/signal"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/ishakdeveloper/surge/shared/authz"
	"github.com/ishakdeveloper/surge/shared/config"
	"github.com/ishakdeveloper/surge/shared/geo"
	"github.com/ishakdeveloper/surge/shared/kafkax"
	commonpb "github.com/ishakdeveloper/surge/shared/proto/common"
	paymentspb "github.com/ishakdeveloper/surge/shared/proto/payments"
	trippb "github.com/ishakdeveloper/surge/shared/proto/trip"
	"github.com/ishakdeveloper/surge/shared/wire"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// Where riders ask from: the simulator's hotspots, so quotes route through
// the same streets its benchmarks do.
var hotspots = []geo.Point{
	{Lat: 52.3791, Lng: 4.9003}, // Centraal
	{Lat: 52.3389, Lng: 4.8723}, // Zuid
	{Lat: 52.3643, Lng: 4.8828}, // Leidseplein
}

func main() {
	if err := run(); err != nil {
		slog.Error("bench-payments failed", "error", err)
		os.Exit(1)
	}
}

type settings struct {
	cluster   kafkax.Cluster
	tripAddr  string
	payAddr   string
	held      bool
	riders    int
	rps       int
	duration  time.Duration
	drain     time.Duration
	out       string
	seed      uint64
	modeLabel string
}

func load() (settings, error) {
	cluster, err := kafkax.ClusterFromEnv()
	if err != nil {
		return settings{}, err
	}
	s := settings{
		cluster:  cluster,
		tripAddr: config.StringOr("TRIP_GRPC_ADDR", "localhost:8110"),
		payAddr:  config.StringOr("PAYMENTS_GRPC_ADDR", "localhost:8112"),
		out:      config.StringOr("BENCH_OUT", ""),
	}
	if s.held, err = config.BoolOr("BENCH_HELD", true); err != nil {
		return s, err
	}
	if s.riders, err = config.IntOr("BENCH_RIDERS", 50); err != nil {
		return s, err
	}
	if s.rps, err = config.IntOr("BENCH_RPS", 5); err != nil {
		return s, err
	}
	if s.rps <= 0 {
		return s, &config.InvalidError{Key: "BENCH_RPS", Value: fmt.Sprint(s.rps), Want: "rate",
			Err: errors.New("want at least one booking a second")}
	}
	if s.duration, err = config.DurationOr("BENCH_DURATION", 2*time.Minute); err != nil {
		return s, err
	}
	if s.drain, err = config.DurationOr("BENCH_DRAIN", 30*time.Second); err != nil {
		return s, err
	}
	seed, err := config.IntOr("BENCH_SEED", 7)
	if err != nil {
		return s, err
	}
	s.seed = uint64(seed)
	s.modeLabel = "unheld"
	if s.held {
		s.modeLabel = "held"
	}
	return s, nil
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	s, err := load()
	if err != nil {
		return err
	}

	tripConn, err := dial(s.tripAddr)
	if err != nil {
		return err
	}
	defer tripConn.Close()
	trips := trippb.NewTripServiceClient(tripConn)

	riders := make([]authz.Identity, s.riders)
	for i := range riders {
		id := uuid.NewString()
		riders[i] = authz.Identity{
			UserID: id, Email: id + "@bench.surge.test", EmailVerified: true, Role: authz.RoleRider,
		}
	}

	if s.held {
		payConn, err := dial(s.payAddr)
		if err != nil {
			return err
		}
		defer payConn.Close()
		// A card per rider before the clock starts. On the fake processor a
		// setup intent attaches the test Visa that always authorizes, so a
		// hold's time is the system's, not a bank's.
		payments := paymentspb.NewPaymentsServiceClient(payConn)
		for _, rider := range riders {
			if _, err := payments.CreateSetupIntent(authz.Outgoing(ctx, rider),
				&paymentspb.CreateSetupIntentRequest{}); err != nil {
				return fmt.Errorf("save a card for %s: %w", rider.UserID, err)
			}
		}
	}

	facts := newFacts()
	reader, err := tail(ctx, s.cluster)
	if err != nil {
		return err
	}
	readerCtx, stopReader := context.WithCancel(ctx)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		facts.read(readerCtx, reader)
	}()

	slog.Info("booking", "mode", s.modeLabel, "rps", s.rps, "for", s.duration, "riders", s.riders)
	book := newBookings()
	started := time.Now()
	var wg sync.WaitGroup
	ticker := time.NewTicker(time.Second / time.Duration(s.rps))
	deadline := time.After(s.duration)
booking:
	for i := 0; ; i++ {
		select {
		case <-ctx.Done():
			break booking
		case <-deadline:
			break booking
		case <-ticker.C:
		}
		rider := riders[i%len(riders)]
		source := rand.New(rand.NewPCG(s.seed, uint64(i)))
		wg.Add(1)
		go func() {
			defer wg.Done()
			book.one(ctx, trips, rider, source)
		}()
	}
	ticker.Stop()
	wg.Wait()
	elapsed := time.Since(started)

	// Until every booking has been dispatched or refused, or the drain runs
	// out — which is itself a finding, and reported as one.
	drainUntil := time.Now().Add(s.drain)
	for time.Now().Before(drainUntil) && ctx.Err() == nil {
		if facts.settled(book.ids()) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	stopReader()
	<-readerDone
	reader.Close()

	cancelled, cancelErrors := book.cancelAll(context.WithoutCancel(ctx), trips)

	result := summarise(s, book, facts, elapsed)
	result.Cancelled, result.CancelErrors = cancelled, cancelErrors
	result.print()
	if s.out != "" {
		encoded, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(s.out, encoded, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", s.out, err)
		}
	}
	return nil
}

func dial(addr string) (*grpc.ClientConn, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	return conn, nil
}

// tail reads the three topics from where they end now, by exact offset.
//
// Not a consumer group: a group resolves "the end" whenever it gets round to
// joining, and a booking made before that would lose its facts. Offsets listed
// up front are the end as of before the first booking.
func tail(ctx context.Context, cluster kafkax.Cluster) (*kgo.Client, error) {
	topics := []string{kafkax.TopicTripLifecycle, kafkax.TopicPaymentEvents, kafkax.TopicGeoEvents}

	admin, err := cluster.Client()
	if err != nil {
		return nil, err
	}
	defer admin.Close()
	ends, err := kadm.NewClient(admin).ListEndOffsets(ctx, topics...)
	if err != nil {
		return nil, fmt.Errorf("list end offsets: %w", err)
	}
	if err := ends.Error(); err != nil {
		return nil, fmt.Errorf("list end offsets: %w", err)
	}

	from := map[string]map[int32]kgo.Offset{}
	ends.Each(func(listed kadm.ListedOffset) {
		if from[listed.Topic] == nil {
			from[listed.Topic] = map[int32]kgo.Offset{}
		}
		from[listed.Topic][listed.Partition] = kgo.NewOffset().At(listed.Offset)
	})

	return cluster.Client(
		kgo.ConsumePartitions(from),
		kgo.FetchMaxWait(50*time.Millisecond),
	)
}

// facts holds the first time each trip reached each stage, from the
// timestamps of the records that say so.
type facts struct {
	mu         sync.Mutex
	requested  map[string]time.Time
	authorized map[string]time.Time
	refused    map[string]string
	dispatched map[string]time.Time
}

func newFacts() *facts {
	return &facts{
		requested:  map[string]time.Time{},
		authorized: map[string]time.Time{},
		refused:    map[string]string{},
		dispatched: map[string]time.Time{},
	}
}

var matchRequested = []byte(wire.TagMatchRequested)

func (f *facts) read(ctx context.Context, reader *kgo.Client) {
	for ctx.Err() == nil {
		fetches := reader.PollFetches(ctx)
		fetches.EachRecord(func(record *kgo.Record) {
			f.mu.Lock()
			defer f.mu.Unlock()
			switch record.Topic {
			case kafkax.TopicTripLifecycle:
				var fact wire.TripFact
				if json.Unmarshal(record.Value, &fact) == nil && fact.Tag == wire.FactTripRequested {
					first(f.requested, fact.TripID, record.Timestamp)
				}
			case kafkax.TopicPaymentEvents:
				var fact wire.PaymentFact
				if json.Unmarshal(record.Value, &fact) != nil {
					return
				}
				switch fact.Tag {
				case wire.FactPaymentAuthorized:
					first(f.authorized, fact.TripID, record.Timestamp)
				case wire.FactPaymentFailed, wire.FactPaymentActionRequired:
					f.refused[fact.TripID] = fact.Tag + ":" + fact.Reason
				}
			case kafkax.TopicGeoEvents:
				// Every ping's cell transition goes through here too; only
				// match requests are decoded.
				if !bytes.Contains(record.Value, matchRequested) {
					return
				}
				var event wire.GeoEvent
				if json.Unmarshal(record.Value, &event) == nil && event.Requested != nil {
					first(f.dispatched, event.Requested.TripID, record.Timestamp)
				}
			}
		})
	}
}

func first(into map[string]time.Time, id string, at time.Time) {
	if _, seen := into[id]; !seen {
		into[id] = at
	}
}

func (f *facts) settled(ids []string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, id := range ids {
		_, dispatched := f.dispatched[id]
		_, refused := f.refused[id]
		if !dispatched && !refused {
			return false
		}
	}
	return true
}

type booking struct {
	rider      authz.Identity
	start, end time.Time
}

type bookings struct {
	mu     sync.Mutex
	byTrip map[string]booking
	errors map[string]int
}

func newBookings() *bookings {
	return &bookings{byTrip: map[string]booking{}, errors: map[string]int{}}
}

func (b *bookings) one(ctx context.Context, trips trippb.TripServiceClient, rider authz.Identity, source *rand.Rand) {
	ctx = authz.Outgoing(ctx, rider)

	// A pickup in the IJ does not route. Another draw is cheaper than a city
	// polygon, which is the simulator's reasoning too.
	var fareID string
	for attempt := 0; attempt < 8 && fareID == ""; attempt++ {
		pickup := pickupPoint(source)
		preview, err := trips.PreviewTrip(ctx, &trippb.PreviewTripRequest{
			Pickup:  coordinate(pickup),
			Dropoff: coordinate(dropoffFrom(pickup, source)),
		})
		if status.Code(err) == codes.InvalidArgument {
			continue
		}
		if err != nil {
			b.fail("preview: " + status.Code(err).String())
			return
		}
		if len(preview.GetFares()) == 0 {
			b.fail("preview: no fares")
			return
		}
		fareID = preview.GetFares()[0].GetFareId()
	}
	if fareID == "" {
		b.fail("preview: no route in 8 draws")
		return
	}

	// The clock starts at the booking call, after the quote: routing a quote
	// is Valhalla's time and the same in both modes.
	start := time.Now()
	created, err := trips.CreateTrip(ctx, &trippb.CreateTripRequest{
		FareId: fareID, IdempotencyKey: uuid.NewString(),
	})
	end := time.Now()
	if err != nil {
		b.fail("create: " + status.Code(err).String())
		return
	}

	b.mu.Lock()
	b.byTrip[created.GetTrip().GetId()] = booking{rider: rider, start: start, end: end}
	b.mu.Unlock()
}

func (b *bookings) fail(reason string) {
	b.mu.Lock()
	b.errors[reason]++
	b.mu.Unlock()
}

func (b *bookings) ids() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	ids := make([]string, 0, len(b.byTrip))
	for id := range b.byTrip {
		ids = append(ids, id)
	}
	return ids
}

// cancelAll cancels every booked trip as its own rider, sixteen at a time.
// A trip dispatched with no matcher running would otherwise wait for a driver
// forever, and in held mode keep money on a card.
func (b *bookings) cancelAll(ctx context.Context, trips trippb.TripServiceClient) (int, int) {
	b.mu.Lock()
	all := make(map[string]booking, len(b.byTrip))
	for id, booked := range b.byTrip {
		all[id] = booked
	}
	b.mu.Unlock()

	var (
		mu        sync.Mutex
		cancelled int
		failed    int
		wg        sync.WaitGroup
	)
	slots := make(chan struct{}, 16)
	for id, booked := range all {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer func() { <-slots; wg.Done() }()
			_, err := trips.CancelTrip(authz.Outgoing(ctx, booked.rider),
				&trippb.CancelTripRequest{TripId: id, Reason: "bench"})
			mu.Lock()
			defer mu.Unlock()
			// A trip payments already refused is cancelled; asking again is
			// not a failure of the cleanup.
			if err == nil || status.Code(err) == codes.FailedPrecondition {
				cancelled++
			} else {
				failed++
			}
		}()
	}
	wg.Wait()
	return cancelled, failed
}

func pickupPoint(source *rand.Rand) geo.Point {
	if source.Float64() < 0.7 {
		return offset(hotspots[source.IntN(len(hotspots))], source.Float64()*800, source.Float64()*2*math.Pi)
	}
	return geo.Amsterdam.Random(source)
}

// dropoffFrom is 1.5 to 4 km away in any direction: a city ride.
func dropoffFrom(pickup geo.Point, source *rand.Rand) geo.Point {
	return offset(pickup, 1500+source.Float64()*2500, source.Float64()*2*math.Pi)
}

func offset(from geo.Point, meters, bearing float64) geo.Point {
	const metersPerDegree = 111_320.0
	return geo.Point{
		Lat: from.Lat + meters*math.Cos(bearing)/metersPerDegree,
		Lng: from.Lng + meters*math.Sin(bearing)/(metersPerDegree*math.Cos(from.Lat*math.Pi/180)),
	}
}

func coordinate(p geo.Point) *commonpb.Coordinate {
	return &commonpb.Coordinate{Lat: p.Lat, Lng: p.Lng}
}

// Stage is one hop's latency distribution, in milliseconds.
type Stage struct {
	Name  string  `json:"name"`
	Count int     `json:"count"`
	Mean  float64 `json:"meanMs"`
	P50   float64 `json:"p50Ms"`
	P95   float64 `json:"p95Ms"`
	P99   float64 `json:"p99Ms"`
	Max   float64 `json:"maxMs"`
}

// Result is one run.
type Result struct {
	Mode         string         `json:"mode"`
	RPS          int            `json:"rps"`
	Seconds      float64        `json:"seconds"`
	Booked       int            `json:"booked"`
	BookedPerSec float64        `json:"bookedPerSecond"`
	Dispatched   int            `json:"dispatched"`
	Refused      map[string]int `json:"refused"`
	Undispatched int            `json:"undispatched"`
	Errors       map[string]int `json:"errors"`
	Stages       []Stage        `json:"stages"`
	Cancelled    int            `json:"cancelled"`
	CancelErrors int            `json:"cancelErrors"`
}

func summarise(s settings, book *bookings, f *facts, elapsed time.Duration) Result {
	book.mu.Lock()
	defer book.mu.Unlock()
	f.mu.Lock()
	defer f.mu.Unlock()

	result := Result{
		Mode: s.modeLabel, RPS: s.rps, Seconds: elapsed.Seconds(),
		Booked: len(book.byTrip), BookedPerSec: float64(len(book.byTrip)) / elapsed.Seconds(),
		Refused: map[string]int{}, Errors: book.errors,
	}

	var call, toRequested, toAuthorized, toDispatch, total []float64
	for id, booked := range book.byTrip {
		call = append(call, ms(booked.end.Sub(booked.start)))
		requested, hasRequested := f.requested[id]
		if hasRequested {
			toRequested = append(toRequested, ms(requested.Sub(booked.start)))
		}
		authorized, hasAuthorized := f.authorized[id]
		if hasAuthorized && hasRequested {
			toAuthorized = append(toAuthorized, ms(authorized.Sub(requested)))
		}
		dispatched, hasDispatched := f.dispatched[id]
		switch {
		case hasDispatched:
			result.Dispatched++
			total = append(total, ms(dispatched.Sub(booked.start)))
			if hasAuthorized {
				toDispatch = append(toDispatch, ms(dispatched.Sub(authorized)))
			}
		case f.refused[id] != "":
			result.Refused[f.refused[id]]++
		default:
			result.Undispatched++
		}
	}

	result.Stages = []Stage{
		stage("booking call", call),
		stage("booked → TripRequested relayed", toRequested),
	}
	if s.held {
		result.Stages = append(result.Stages,
			stage("TripRequested → PaymentAuthorized relayed", toAuthorized),
			stage("PaymentAuthorized → MatchRequested", toDispatch))
	}
	result.Stages = append(result.Stages, stage("booked → MatchRequested", total))
	return result
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func stage(name string, values []float64) Stage {
	out := Stage{Name: name, Count: len(values)}
	if len(values) == 0 {
		return out
	}
	slices.Sort(values)
	var sum float64
	for _, v := range values {
		sum += v
	}
	out.Mean = sum / float64(len(values))
	out.P50, out.P95, out.P99 = quantile(values, 0.5), quantile(values, 0.95), quantile(values, 0.99)
	out.Max = values[len(values)-1]
	return out
}

// quantile is nearest-rank over sorted values.
func quantile(sorted []float64, q float64) float64 {
	rank := int(math.Ceil(q*float64(len(sorted)))) - 1
	return sorted[max(0, min(rank, len(sorted)-1))]
}

func (r Result) print() {
	fmt.Printf("\n%s: %d booked in %.0fs (%.2f/s of %d asked), %d dispatched, %d undispatched, refused %v, errors %v, cancelled %d (%d failed)\n\n",
		r.Mode, r.Booked, r.Seconds, r.BookedPerSec, r.RPS, r.Dispatched, r.Undispatched, r.Refused, r.Errors,
		r.Cancelled, r.CancelErrors)
	fmt.Println("| stage | n | mean ms | p50 ms | p95 ms | p99 ms | max ms |")
	fmt.Println("| --- | ---: | ---: | ---: | ---: | ---: | ---: |")
	for _, s := range r.Stages {
		fmt.Printf("| %s | %d | %.1f | %.1f | %.1f | %.1f | %.1f |\n", s.Name, s.Count, s.Mean, s.P50, s.P95, s.P99, s.Max)
	}
}
