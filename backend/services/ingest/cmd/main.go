// Command ingest consumes GPS pings into the in-memory position index.
//
// Phase 1's version is deliberately a stub: it exists to measure, not to serve.
// What it proves is that the position index belongs in memory — and what it
// measures is the number that matters more than consumer lag, which is how
// stale the index is relative to when a driver actually reported.
//
// Lag says how many records are outstanding. End-to-end age says how out of
// date the map is. They diverge exactly when it matters: a consumer keeping up
// with a backlog shows low lag and high age.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ishakdeveloper/surge/services/ingest/internal/domain"
	"github.com/ishakdeveloper/surge/services/ingest/internal/infrastructure/events"
	debughttp "github.com/ishakdeveloper/surge/services/ingest/internal/infrastructure/http"
	"github.com/ishakdeveloper/surge/shared/config"
	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/ishakdeveloper/surge/shared/obs"
	"github.com/ishakdeveloper/surge/shared/tracing"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"
)

func main() {
	if err := run(); err != nil {
		slog.Error("ingest exited", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cluster, err := kafkax.ClusterFromEnv()
	if err != nil {
		return err
	}
	group := config.StringOr("INGEST_GROUP", "ingest")
	debugAddr := config.StringOr("INGEST_DEBUG_ADDR", ":8102")
	metricsAddr := config.StringOr("INGEST_METRICS_ADDR", ":9102")

	if err := kafkax.EnsureTopics(ctx, cluster); err != nil {
		return err
	}

	shutdownTracing, err := tracing.Init(ctx, "ingest",
		config.StringOr("OTEL_EXPORTER_OTLP_ENDPOINT", ""))
	if err != nil {
		return err
	}
	defer func() {
		flush, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = shutdownTracing(flush)
	}()

	registry := obs.NewRegistry("ingest")
	metrics := newMetrics(registry)
	index := domain.NewIndex()

	// Start at the end of the log, not the beginning.
	//
	// A position is worth something for about as long as it takes the next ping
	// to arrive. Replaying six hours of retained pings on startup would spend
	// minutes reconstructing where drivers used to be, produce a live-looking
	// index full of ghosts, and then be overwritten anyway within one ping
	// interval — while the map showed a city that had already moved on.
	//
	// The index is derived state with a four-second rebuild time, so the honest
	// recovery strategy is to wait four seconds rather than to replay. This is
	// the same reasoning that keeps driver positions out of the matcher's
	// compacted checkpoint in Phase 2.
	// The re-keying hop: pings arrive keyed by driver, geo events leave keyed by
	// shard cell.
	producer, err := kafkax.NewProducer(cluster)
	if err != nil {
		return err
	}
	defer producer.Close()

	client, err := kafkax.NewConsumerGroup(cluster, group, []string{kafkax.TopicLocPing},
		kgo.ConsumeResetOffset(kgo.NewOffset().AtEnd()))
	if err != nil {
		return err
	}
	defer client.Close()

	pickupSeconds := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "surge_ingest_pickup_seconds",
		Help:    "Observed pickups: from a driver setting off to the rider getting in.",
		Buckets: []float64{30, 60, 90, 120, 180, 240, 300, 420, 600, 900},
	})
	registry.MustRegister(pickupSeconds)

	errs := make(chan error, 2)
	consumer := events.NewConsumer(client, producer, index, events.Hooks{
		OnPickup:   func(seconds, _ float64) { pickupSeconds.Observe(seconds) },
		OnIndexed:  func() { metrics.consumed.Inc() },
		OnRejected: func(reason string) { metrics.rejected.WithLabelValues(reason).Inc() },
		OnStale: func(duplicate bool) {
			kind := "reordered"
			if duplicate {
				kind = "duplicate"
			}
			metrics.staleByKind.WithLabelValues(kind).Inc()
			metrics.stale.Inc()
		},
		OnIndexChanged:  func() { metrics.transitions.Inc() },
		OnShardHandover: func() { metrics.handovers.Inc() },
		OnProduced:      func(tag string) { metrics.produced.WithLabelValues(tag).Inc() },
		OnProduceError:  func() { metrics.produceErrors.Inc() },
		OnAge:           func(seconds float64) { metrics.age.Observe(seconds) },
		OnProcess:       func(seconds float64) { metrics.process.Observe(seconds) },
		OnSnapshot: func(stats domain.Stats) {
			metrics.drivers.Set(float64(stats.Drivers))
			metrics.cells.Set(float64(stats.Cells))
			metrics.shards.Set(float64(stats.Shards))
		},
	})

	go func() { errs <- registry.ServeMetrics(ctx, metricsAddr) }()
	go func() { errs <- debughttp.NewServer(debugAddr, index).Run(ctx) }()
	go func() { errs <- consumer.Run(ctx) }()

	select {
	case <-ctx.Done():
		return nil
	case err := <-errs:
		return err
	}
}

type metrics struct {
	consumed      prometheus.Counter
	rejected      *prometheus.CounterVec
	stale         prometheus.Counter
	staleByKind   *prometheus.CounterVec
	transitions   prometheus.Counter
	handovers     prometheus.Counter
	produced      *prometheus.CounterVec
	produceErrors prometheus.Counter
	drivers       prometheus.Gauge
	cells         prometheus.Gauge
	shards        prometheus.Gauge
	age           prometheus.Histogram
	process       prometheus.Histogram
}

func newMetrics(registry *obs.Registry) *metrics {
	m := &metrics{
		consumed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "surge_ingest_pings_total", Help: "Pings indexed.",
		}),
		rejected: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "surge_ingest_rejected_total", Help: "Pings dropped, by reason.",
		}, []string{"reason"}),
		stale: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "surge_ingest_stale_total", Help: "Pings dropped for arriving out of order.",
		}),
		staleByKind: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "surge_ingest_stale_by_kind_total",
			Help: "Stale pings split by cause. `duplicate` is at-least-once delivery working; `reordered` should be zero.",
		}, []string{"kind"}),
		transitions: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "surge_ingest_cell_transitions_total",
			Help: "Drivers crossing a resolution-9 index cell boundary.",
		}),
		handovers: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "surge_ingest_shard_handovers_total",
			Help: "Drivers crossing a resolution-7 shard boundary — each one is a leave and an entry on two different partitions.",
		}),
		produced: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "surge_ingest_geo_events_total",
			Help: "Events produced to geo.events, by tag.",
		}, []string{"tag"}),
		produceErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "surge_ingest_produce_errors_total",
			Help: "Geo events that failed to reach the broker.",
		}),
		drivers: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "surge_ingest_drivers", Help: "Drivers held in the index.",
		}),
		cells: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "surge_ingest_index_cells", Help: "Occupied index cells.",
		}),
		shards: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "surge_ingest_shard_cells", Help: "Occupied shard cells.",
		}),
		age: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "surge_ingest_ping_age_seconds",
			Help: "Time from a driver emitting a ping to it being indexed.",
			// Hand-picked rather than DefBuckets: the interesting range is 1ms
			// to 30s, and the default buckets stop at 10s — which is where this
			// gets interesting rather than where it stops mattering.
			Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30},
		}),
		process: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "surge_ingest_process_seconds",
			Help:    "Time to index one ping, excluding transport.",
			Buckets: []float64{1e-6, 5e-6, 1e-5, 5e-5, 1e-4, 5e-4, 1e-3, 5e-3, 1e-2},
		}),
	}

	registry.MustRegister(m.consumed, m.rejected, m.stale, m.staleByKind, m.transitions, m.handovers,
		m.produced, m.produceErrors, m.drivers, m.cells, m.shards, m.age, m.process)
	return m
}
