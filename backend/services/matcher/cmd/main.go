// Command matcher is the geo-sharded matching engine.
//
// Run several of them. Kafka's consumer group assigns partitions, each
// partition is a set of resolution-7 cells, and each set gets one goroutine
// that is the only writer for every driver standing in it. Scaling out is
// adding a process; rebalancing is Kafka's job, and what this service adds is
// making sure the promises survive it.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/ishakdeveloper/surge/services/matcher/internal/domain"
	"github.com/ishakdeveloper/surge/services/matcher/internal/infrastructure/events"
	"github.com/ishakdeveloper/surge/shared/config"
	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/ishakdeveloper/surge/shared/obs"
	"github.com/ishakdeveloper/surge/shared/routing"
	"github.com/ishakdeveloper/surge/shared/tracing"
	"github.com/ishakdeveloper/surge/shared/wire"
	"github.com/prometheus/client_golang/prometheus"
)

func main() {
	if err := run(); err != nil {
		slog.Error("matcher exited", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	brokers := config.Strings("KAFKA_BROKERS", []string{"localhost:19092"})
	group := config.StringOr("MATCHER_GROUP", "matcher")
	metricsAddr := config.StringOr("MATCHER_METRICS_ADDR", ":9103")

	settings := domain.DefaultConfig()

	var err error
	if settings.SearchRings, err = config.IntOr("MATCHER_SEARCH_RINGS", settings.SearchRings); err != nil {
		return err
	}
	if settings.MaxCandidates, err = config.IntOr("MATCHER_MAX_CANDIDATES", settings.MaxCandidates); err != nil {
		return err
	}
	if settings.OfferTTL, err = config.DurationOr("MATCHER_OFFER_TTL", settings.OfferTTL); err != nil {
		return err
	}
	// greedy | batched. A runtime switch rather than a build, so the two can be
	// compared on the same fleet, the same seed and the same demand.
	if settings.Strategy, err = domain.ParseStrategy(config.StringOr("MATCH_STRATEGY", string(settings.Strategy))); err != nil {
		return err
	}
	if settings.BatchWindow, err = config.DurationOr("MATCHER_BATCH_WINDOW", settings.BatchWindow); err != nil {
		return err
	}

	// Whether a driver must be approved by the fleet to be offered work.
	//
	// Off by default, and the default is the honest one: the simulator invents
	// forty thousand drivers who never uploaded a licence, and every benchmark
	// in docs/benchmarks was run on them. A deployment carrying real drivers
	// turns it on — deploy/k8s does — and what is running either way is stated
	// at startup rather than left to be inferred from a match rate of zero.
	requireApproval, err := config.BoolOr("MATCHER_REQUIRE_APPROVAL", false)
	if err != nil {
		return err
	}

	if err := kafkax.EnsureTopics(ctx, brokers); err != nil {
		return err
	}

	partitions, err := kafkax.TopicPartitions(ctx, brokers, kafkax.TopicGeoEvents)
	if err != nil {
		return err
	}
	// Who the fleet has approved, read in full before anything is matched.
	// Nothing is gated until it has been: an empty roster and a strict matcher
	// is a city with no drivers in it.
	var roster *events.Roster
	if requireApproval {
		if roster, err = events.NewRoster(ctx, brokers); err != nil {
			return err
		}
		defer roster.Close()
		settings.Approved = roster.Approved
	}

	// A partition count that has drifted from the constant silently changes how
	// many shards exist, so it is stated at startup rather than assumed.
	slog.Info("shard topology",
		"partitions", partitions,
		"expected", kafkax.GeoPartitions,
		"offerTTL", settings.OfferTTL,
		"searchRings", settings.SearchRings,
		"strategy", settings.Strategy,
		"batchWindow", settings.BatchWindow,
		"requireApproval", requireApproval,
		"driversKnown", rosterSize(roster),
	)

	shutdownTracing, err := tracing.Init(ctx, "matcher",
		config.StringOr("OTEL_EXPORTER_OTLP_ENDPOINT", ""))
	if err != nil {
		return err
	}
	defer func() {
		flush, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = shutdownTracing(flush)
	}()

	registry := obs.NewRegistry("matcher")
	metrics := newMetrics(registry, settings.Strategy)

	producer, err := kafkax.NewProducer(brokers)
	if err != nil {
		return err
	}
	defer producer.Close()

	// Named on every frame, so the console can show which instance holds which
	// slice of the city, and watch it move during a rebalance.
	instance := config.StringOr("HOSTNAME", fmt.Sprintf("matcher-%d", os.Getpid()))

	// Travel times for batched matching, fetched off the shard loop (see
	// Runner.solve). Greedy never calls it.
	router := routing.New(config.StringOr("VALHALLA_URL", "http://localhost:8002"))

	// Off unless set: every offer's car and rider positions, for a benchmark to
	// price over real roads once the run is over.
	dispatches, err := openDispatchLog(config.StringOr("MATCHER_DISPATCH_LOG", ""), settings.Strategy)
	if err != nil {
		return err
	}
	defer dispatches.Close()

	runner := events.NewRunner(brokers, settings, producer, router, instance, events.Hooks{
		OnMatched: func(result domain.MatchResult) {
			metrics.matched.Inc()
			metrics.latency.Observe(result.Latency.Seconds())
		},
		OnAbandoned: func() { metrics.abandoned.Inc() },
		OnRejection: func(reason wire.ReserveRejection) {
			metrics.rejections.WithLabelValues(string(reason)).Inc()
		},
		OnEvent: func(tag string) { metrics.events.WithLabelValues(tag).Inc() },
		OnDispatched: func(dispatch domain.Dispatch) {
			metrics.pickup.Observe(dispatch.Meters)
			dispatches.write(dispatch)
		},
		OnBatch: func(requests, _ int, took time.Duration, err error) {
			fetched := "ok"
			if err != nil {
				// Solved over distance instead; counted rather than logged,
				// because a router outage would be one line per shard per window.
				fetched = "no_travel_times"
			}
			metrics.batchSize.Observe(float64(requests))
			metrics.batchFetch.WithLabelValues(fetched).Observe(took.Seconds())
		},
		OnOwnership: func(total int, changed []int32, gained bool) {
			metrics.owned.Set(float64(total))
			direction := "revoked"
			if gained {
				direction = "assigned"
			}
			metrics.rebalances.WithLabelValues(direction).Add(float64(len(changed)))
		},
		OnRestoreStall: func(partition int32, took time.Duration, offers int) {
			// The real cost of a rebalance, and the reason it is a first-class
			// metric: a shard cannot match until it knows which drivers are
			// already spoken for, so this is dead time for that slice of the
			// city.
			metrics.restoreStall.Observe(took.Seconds())
			metrics.restoredOffers.Add(float64(offers))
			slog.Info("shard restored",
				"partition", partition, "took", took.Round(time.Millisecond), "offers", offers)
		},
		OnShardState: func(partition int32, drivers, pending, offers int) {
			label := partitionLabel(partition)
			metrics.shardDrivers.WithLabelValues(label).Set(float64(drivers))
			metrics.shardPending.WithLabelValues(label).Set(float64(pending))
			metrics.shardOffers.WithLabelValues(label).Set(float64(offers))
		},
		OnHandlerError:   func(err error) { metrics.errors.WithLabelValues("handle").Inc() },
		OnProduceFailure: func(err error) { metrics.errors.WithLabelValues("produce").Inc() },
	})

	errs := make(chan error, 3)
	go func() { errs <- registry.ServeMetrics(ctx, metricsAddr) }()
	go func() { errs <- runner.Run(ctx, group) }()
	if roster != nil {
		go func() { errs <- roster.Run(ctx) }()
	}

	select {
	case <-ctx.Done():
		// Run checkpoints on its way out; give it a moment to finish.
		time.Sleep(2 * time.Second)
		return nil
	case err := <-errs:
		return err
	}
}

// rosterSize is how many drivers the fleet has spoken about, or -1 when the
// matcher is not gating at all — a zero there would read as "nobody approved".
func rosterSize(roster *events.Roster) int {
	if roster == nil {
		return -1
	}
	return roster.Size()
}

func partitionLabel(partition int32) string {
	return string(rune('0'+partition/10)) + string(rune('0'+partition%10))
}

type metrics struct {
	matched        prometheus.Counter
	abandoned      prometheus.Counter
	latency        prometheus.Histogram
	rejections     *prometheus.CounterVec
	events         *prometheus.CounterVec
	owned          prometheus.Gauge
	rebalances     *prometheus.CounterVec
	restoreStall   prometheus.Histogram
	restoredOffers prometheus.Counter
	shardDrivers   *prometheus.GaugeVec
	shardPending   *prometheus.GaugeVec
	shardOffers    *prometheus.GaugeVec
	errors         *prometheus.CounterVec
	batchSize      prometheus.Histogram
	pickup         prometheus.Histogram
	batchFetch     *prometheus.HistogramVec
}

func newMetrics(registry *obs.Registry, strategy domain.Strategy) *metrics {
	// The strategy rides on the outcome metrics as a constant label, so a
	// greedy run and a batched run land as two series of the same metric and
	// one query compares them.
	byStrategy := prometheus.Labels{"strategy": string(strategy)}

	m := &metrics{
		matched: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "surge_matcher_matched_total", Help: "Trips matched to a driver.",
			ConstLabels: byStrategy,
		}),
		abandoned: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "surge_matcher_abandoned_total", Help: "Requests that found nobody.",
			ConstLabels: byStrategy,
		}),
		latency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:        "surge_matcher_match_latency_seconds",
			ConstLabels: byStrategy,
			Help:        "Rider request to driver accepted, end to end.",
			// The headline number. Buckets chosen for a range where a human is
			// waiting: sub-second is excellent, ten seconds is a bad day.
			Buckets: []float64{.05, .1, .25, .5, 1, 2, 3, 5, 8, 12, 20, 45},
		}),
		rejections: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "surge_matcher_rejections_total",
			Help: "Reservations refused, by reason. `busy` is the contention signal.",
		}, []string{"reason"}),
		events: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "surge_matcher_events_total", Help: "Geo events handled, by tag.",
		}, []string{"tag"}),
		owned: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "surge_matcher_partitions_owned",
			Help: "Partitions this instance owns — the shape of the current assignment.",
		}),
		rebalances: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "surge_matcher_partition_moves_total",
			Help: "Partitions gained or given up. Every one is a slice of the city changing owner.",
		}, []string{"direction"}),
		restoreStall: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "surge_matcher_restore_stall_seconds",
			Help:    "Time a partition spends restoring its checkpoint before it can match. The real cost of a rebalance.",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
		}),
		restoredOffers: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "surge_matcher_restored_offers_total",
			Help: "Reservations recovered from a checkpoint across a rebalance.",
		}),
		shardDrivers: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "surge_matcher_shard_drivers", Help: "Drivers held per partition.",
		}, []string{"partition"}),
		shardPending: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "surge_matcher_shard_pending", Help: "Requests in flight per partition.",
		}, []string{"partition"}),
		shardOffers: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "surge_matcher_shard_offers", Help: "Offers outstanding per partition.",
		}, []string{"partition"}),
		errors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "surge_matcher_errors_total", Help: "Failures, by stage.",
		}, []string{"stage"}),
		pickup: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:        "surge_matcher_pickup_meters",
			Help:        "Straight-line distance from an offered car to its rider. What batched matching is for.",
			ConstLabels: byStrategy,
			Buckets:     []float64{100, 250, 500, 750, 1000, 1500, 2000, 3000, 5000},
		}),
		batchSize: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "surge_matcher_batch_requests",
			Help:    "Riders per batched solve. One means the window saw no contention to solve.",
			Buckets: []float64{1, 2, 3, 5, 8, 13, 21, 32},
		}),
		batchFetch: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "surge_matcher_batch_fetch_seconds",
			Help:    "Travel-time matrix fetch per batch, by whether the router answered.",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 3},
		}, []string{"fetched"}),
	}

	registry.MustRegister(m.matched, m.abandoned, m.latency, m.rejections, m.events,
		m.owned, m.rebalances, m.restoreStall, m.restoredOffers,
		m.shardDrivers, m.shardPending, m.shardOffers, m.errors, m.batchSize, m.batchFetch, m.pickup)
	return m
}

// dispatchLog writes each offer's car and rider positions as JSON lines.
//
// For `make bench-matching`, which prices a sample of them over real roads
// once the run is over. Pricing during the run would compete with the batched
// strategy for Valhalla, and measure the measurement as much as the matcher.
type dispatchLog struct {
	mu       sync.Mutex
	file     *os.File
	out      *bufio.Writer
	strategy domain.Strategy
}

func openDispatchLog(path string, strategy domain.Strategy) (*dispatchLog, error) {
	if path == "" {
		return nil, nil
	}
	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("matcher: dispatch log: %w", err)
	}
	log := &dispatchLog{file: file, out: bufio.NewWriterSize(file, 64<<10), strategy: strategy}
	// Flushed every second, not only on Close: a benchmark stops the matcher
	// with a signal, and a buffer lost to a hard stop is a run without a result.
	go func() {
		for range time.Tick(time.Second) {
			log.mu.Lock()
			_ = log.out.Flush()
			log.mu.Unlock()
		}
	}()
	return log, nil
}

func (l *dispatchLog) write(dispatch domain.Dispatch) {
	if l == nil {
		return
	}
	line, err := json.Marshal(map[string]any{
		"atMs": time.Now().UnixMilli(), "strategy": l.strategy,
		"tripId": dispatch.TripID, "driverId": dispatch.DriverID,
		"driverLat": dispatch.Driver.Lat, "driverLng": dispatch.Driver.Lng,
		"pickupLat": dispatch.Pickup.Lat, "pickupLng": dispatch.Pickup.Lng,
		"meters": dispatch.Meters,
	})
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.out.Write(append(line, '\n'))
}

func (l *dispatchLog) Close() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.out.Flush()
	_ = l.file.Close()
}
