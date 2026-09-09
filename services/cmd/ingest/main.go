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
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ishakdeveloper/surge/internal/ingest"
	"github.com/ishakdeveloper/surge/pkg/config"
	"github.com/ishakdeveloper/surge/pkg/kafkax"
	"github.com/ishakdeveloper/surge/pkg/obs"
	"github.com/ishakdeveloper/surge/pkg/wire"
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

	brokers := config.Strings("KAFKA_BROKERS", []string{"localhost:19092"})
	group := config.StringOr("INGEST_GROUP", "ingest")
	debugAddr := config.StringOr("INGEST_DEBUG_ADDR", ":8102")
	metricsAddr := config.StringOr("INGEST_METRICS_ADDR", ":9102")

	if err := kafkax.EnsureTopics(ctx, brokers); err != nil {
		return err
	}

	registry := obs.NewRegistry("ingest")
	metrics := newMetrics(registry)
	index := ingest.NewIndex()

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
	client, err := kafkax.NewConsumerGroup(brokers, group, []string{kafkax.TopicLocPing},
		kgo.ConsumeResetOffset(kgo.NewOffset().AtEnd()))
	if err != nil {
		return err
	}
	defer client.Close()

	errs := make(chan error, 2)
	go func() { errs <- registry.ServeMetrics(ctx, metricsAddr) }()
	go func() { errs <- serveDebug(ctx, debugAddr, index) }()

	go func() { errs <- consume(ctx, client, index, metrics) }()

	select {
	case <-ctx.Done():
		return nil
	case err := <-errs:
		return err
	}
}

func consume(ctx context.Context, client *kgo.Client, index *ingest.Index, metrics *metrics) error {
	// Offsets are committed on a timer rather than per record. Per-record
	// commits at 10k/sec would be 10k commit requests a second — more traffic
	// than the pings themselves — and the cost of a crash here is replaying a
	// few seconds of positions that are about to be overwritten anyway.
	commit := time.NewTicker(5 * time.Second)
	defer commit.Stop()

	// Gauges are sampled on a timer, NOT once per poll.
	//
	// They used to be updated at the bottom of the poll loop, and that was the
	// first thing to break under load: Stats() walks every driver to count
	// distinct shards, so at 20,000 drivers and a few hundred polls a second it
	// was several million map operations a second spent entirely on telemetry.
	// End-to-end ping age went from 25ms to over 30 seconds, the consumer
	// missed its session timeout, and the resulting rebalance replayed records
	// — which showed up as thousands of "stale" pings and looked like an
	// ordering bug rather than an observer effect.
	sample := time.NewTicker(time.Second)
	defer sample.Stop()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-commit.C:
				if err := client.CommitUncommittedOffsets(ctx); err != nil {
					slog.Warn("commit failed", "error", err)
				}
			case <-sample.C:
				stats := index.Stats()
				metrics.drivers.Set(float64(stats.Drivers))
				metrics.cells.Set(float64(stats.Cells))
				metrics.shards.Set(float64(stats.Shards))
			}
		}
	}()

	for {
		if err := ctx.Err(); err != nil {
			return nil
		}

		fetches := client.PollRecords(ctx, 10_000)
		if fetches.IsClientClosed() {
			return nil
		}

		fetches.EachError(func(topic string, partition int32, err error) {
			if !errors.Is(err, context.Canceled) {
				slog.Warn("fetch error", "topic", topic, "partition", partition, "error", err)
			}
		})

		fetches.EachRecord(func(record *kgo.Record) {
			var ping wire.DriverPing
			if err := json.Unmarshal(record.Value, &ping); err != nil {
				metrics.rejected.WithLabelValues("malformed").Inc()
				return
			}
			if !ping.Valid() {
				metrics.rejected.WithLabelValues("invalid").Inc()
				return
			}

			// Measured from when the driver said it sent, not from the Kafka
			// timestamp: the whole point is the age of the map as a rider would
			// experience it, which includes time spent in the broker.
			age := time.Since(time.UnixMilli(ping.SentAtMs)).Seconds()
			metrics.age.Observe(age)

			started := time.Now()
			observation, err := index.Observe(ping)
			metrics.process.Observe(time.Since(started).Seconds())

			if err != nil {
				metrics.rejected.WithLabelValues("uncellable").Inc()
				return
			}

			switch {
			case observation.Stale:
				metrics.stale.Inc()
			case observation.Transition:
				metrics.transitions.Inc()
			}

			metrics.consumed.Inc()
		})
	}
}

type metrics struct {
	consumed    prometheus.Counter
	rejected    *prometheus.CounterVec
	stale       prometheus.Counter
	transitions prometheus.Counter
	drivers     prometheus.Gauge
	cells       prometheus.Gauge
	shards      prometheus.Gauge
	age         prometheus.Histogram
	process     prometheus.Histogram
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
		transitions: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "surge_ingest_cell_transitions_total",
			Help: "Drivers crossing an index cell boundary — the rate at which shards will hand drivers over.",
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

	registry.MustRegister(m.consumed, m.rejected, m.stale, m.transitions,
		m.drivers, m.cells, m.shards, m.age, m.process)
	return m
}

func serveDebug(ctx context.Context, addr string, index *ingest.Index) error {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /debug/stats", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, index.Stats())
	})

	// The shard load distribution: how evenly geography spreads drivers, which
	// decides whether sharding by cell is balanced or whether the city centre
	// becomes one hot partition.
	mux.HandleFunc("GET /debug/shards", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, index.ShardLoad())
	})

	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()

	slog.Info("debug api listening", "addr", addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("ingest: debug api: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}
