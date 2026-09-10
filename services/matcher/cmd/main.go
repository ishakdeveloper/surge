// Command matcher is the geo-sharded matching engine.
//
// Run several of them. Kafka's consumer group assigns partitions, each
// partition is a set of resolution-7 cells, and each set gets one goroutine
// that is the only writer for every driver standing in it. Scaling out is
// adding a process; rebalancing is Kafka's job, and what this service adds is
// making sure the promises survive it.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ishakdeveloper/surge/matcher/internal/domain"
	"github.com/ishakdeveloper/surge/matcher/internal/infrastructure/events"
	"github.com/ishakdeveloper/surge/shared/config"
	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/ishakdeveloper/surge/shared/obs"
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

	if err := kafkax.EnsureTopics(ctx, brokers); err != nil {
		return err
	}

	partitions, err := kafkax.TopicPartitions(ctx, brokers, kafkax.TopicGeoEvents)
	if err != nil {
		return err
	}
	// A partition count that has drifted from the constant silently changes how
	// many shards exist, so it is stated at startup rather than assumed.
	slog.Info("shard topology",
		"partitions", partitions,
		"expected", kafkax.GeoPartitions,
		"offerTTL", settings.OfferTTL,
		"searchRings", settings.SearchRings,
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
	metrics := newMetrics(registry)

	producer, err := kafkax.NewProducer(brokers)
	if err != nil {
		return err
	}
	defer producer.Close()

	runner := events.NewRunner(brokers, settings, producer, events.Hooks{
		OnMatched: func(result domain.MatchResult) {
			metrics.matched.Inc()
			metrics.latency.Observe(result.Latency.Seconds())
		},
		OnAbandoned: func() { metrics.abandoned.Inc() },
		OnRejection: func(reason wire.ReserveRejection) {
			metrics.rejections.WithLabelValues(string(reason)).Inc()
		},
		OnEvent: func(tag string) { metrics.events.WithLabelValues(tag).Inc() },
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

	errs := make(chan error, 2)
	go func() { errs <- registry.ServeMetrics(ctx, metricsAddr) }()
	go func() { errs <- runner.Run(ctx, group) }()

	select {
	case <-ctx.Done():
		// Run checkpoints on its way out; give it a moment to finish.
		time.Sleep(2 * time.Second)
		return nil
	case err := <-errs:
		return err
	}
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
}

func newMetrics(registry *obs.Registry) *metrics {
	m := &metrics{
		matched: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "surge_matcher_matched_total", Help: "Trips matched to a driver.",
		}),
		abandoned: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "surge_matcher_abandoned_total", Help: "Requests that found nobody.",
		}),
		latency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "surge_matcher_match_latency_seconds",
			Help: "Rider request to driver accepted, end to end.",
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
	}

	registry.MustRegister(m.matched, m.abandoned, m.latency, m.rejections, m.events,
		m.owned, m.rebalances, m.restoreStall, m.restoredOffers,
		m.shardDrivers, m.shardPending, m.shardOffers, m.errors)
	return m
}
