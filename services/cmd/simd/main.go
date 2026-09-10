// Command simd is the driver simulator daemon.
//
// It is built before the product on purpose: it is the load generator that
// makes every later measurement real, and the knob the demo turns.
//
//	POST /sim/config  {"drivers":10000,"pingIntervalMs":4000}
//	GET  /sim/stats
//
// Metrics are on a separate port so Prometheus scrapes the process even while
// the control API is being hammered.
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
	"strconv"
	"syscall"
	"time"

	"github.com/ishakdeveloper/surge/internal/sim"
	"github.com/ishakdeveloper/surge/pkg/config"
	"github.com/ishakdeveloper/surge/pkg/geo"
	"github.com/ishakdeveloper/surge/pkg/kafkax"
	"github.com/ishakdeveloper/surge/pkg/obs"
	"github.com/ishakdeveloper/surge/pkg/routing"
	"github.com/ishakdeveloper/surge/pkg/tracing"
	"github.com/prometheus/client_golang/prometheus"
)

func main() {
	if err := run(); err != nil {
		slog.Error("simd exited", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	brokers := config.Strings("KAFKA_BROKERS", []string{"localhost:19092"})
	valhallaURL := config.StringOr("VALHALLA_URL", "http://localhost:8002")
	controlAddr := config.StringOr("SIM_CONTROL_ADDR", ":8101")
	metricsAddr := config.StringOr("SIM_METRICS_ADDR", ":9101")

	poolTarget, err := config.IntOr("SIM_ROUTE_POOL", 400)
	if err != nil {
		return err
	}
	poolConcurrency, err := config.IntOr("SIM_ROUTE_CONCURRENCY", 8)
	if err != nil {
		return err
	}
	useKafka, err := config.BoolOr("SIM_KAFKA", true)
	if err != nil {
		return err
	}

	riderSettings := sim.DefaultRiderConfig()
	if riderSettings.RequestsPerSecond, err = floatOr("SIM_REQUESTS_PER_SECOND", riderSettings.RequestsPerSecond); err != nil {
		return err
	}
	if riderSettings.AcceptRate, err = floatOr("SIM_ACCEPT_RATE", riderSettings.AcceptRate); err != nil {
		return err
	}
	if riderSettings.AcceptLatency, err = config.DurationOr("SIM_ACCEPT_LATENCY", riderSettings.AcceptLatency); err != nil {
		return err
	}

	settings := sim.DefaultConfig()
	if settings.Drivers, err = config.IntOr("SIM_DRIVERS", settings.Drivers); err != nil {
		return err
	}
	if settings.PingInterval, err = config.DurationOr("SIM_PING_INTERVAL", settings.PingInterval); err != nil {
		return err
	}

	shutdownTracing, err := tracing.Init(ctx, "simd",
		config.StringOr("OTEL_EXPORTER_OTLP_ENDPOINT", ""))
	if err != nil {
		return err
	}
	defer func() {
		flush, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = shutdownTracing(flush)
	}()

	registry := obs.NewRegistry("simd")
	metrics := newMetrics(registry)

	router := routing.New(valhallaURL)
	if err := router.Healthy(ctx); err != nil {
		return fmt.Errorf("simd: %w", err)
	}

	var transport sim.Transport
	if useKafka {
		if err := kafkax.EnsureTopics(ctx, brokers); err != nil {
			return err
		}
		kafka, err := sim.NewKafkaTransport(brokers, func(error) { metrics.pingErrors.Inc() })
		if err != nil {
			return err
		}
		transport = kafka
	} else {
		// The control run: measures what the simulator itself costs, so a
		// throughput ceiling can be attributed rather than guessed at. Counting
		// is left to the fleet hook below — doing it here as well would double
		// every ping and make the control run look twice as fast as it is.
		transport = sim.NewDiscardTransport(nil)
		slog.Warn("SIM_KAFKA=false: pings are discarded, this measures the simulator only")
	}
	defer transport.Close()

	pool := sim.NewRoutePool(router, geo.Amsterdam, poolTarget)
	pool.OnSize(func(size int) { metrics.poolSize.Set(float64(size)) })
	pool.OnFetch(func(duration time.Duration, err error) {
		outcome := "ok"
		if err != nil {
			outcome = "no_route"
		}
		metrics.routeFetch.WithLabelValues(outcome).Observe(duration.Seconds())
	})

	slog.Info("filling route pool", "target", poolTarget, "concurrency", poolConcurrency)
	started := time.Now()
	// A quarter of the target is enough to start: drivers spread over 100
	// distinct routes already look like traffic, and the pool keeps filling
	// underneath them.
	if err := pool.Fill(ctx, max(1, poolTarget/4), poolConcurrency); err != nil {
		return err
	}
	slog.Info("route pool ready", "routes", pool.Size(), "took", time.Since(started).Round(time.Millisecond))

	go pool.Run(ctx, settings.Seed)

	fleet, err := sim.New(pool, transport, settings, sim.Hooks{
		OnPing:      func() { metrics.pings.Inc() },
		OnPingError: func(error) { metrics.pingErrors.Inc() },
		OnArrival:   func() { metrics.arrivals.Inc() },
		OnDrivers:   func(count int) { metrics.drivers.Set(float64(count)) },
	})
	if err != nil {
		return err
	}

	if err := fleet.Scale(ctx, settings); err != nil {
		return err
	}
	slog.Info("fleet started",
		"drivers", settings.Drivers,
		"pingInterval", settings.PingInterval,
		"pingsPerSecond", float64(settings.Drivers)/settings.PingInterval.Seconds(),
	)

	// Demand, and the drivers who answer it. Only with Kafka: the discard
	// transport is a control run measuring the simulator itself, and there is
	// nothing on the other end to match against.
	if useKafka {
		riderSettings.Seed = settings.Seed
		riders, err := sim.NewRiders(brokers, config.StringOr("SIM_RIDER_GROUP", "sim-drivers"), pool, riderSettings,
			sim.RiderHooks{
				OnRequest:  func() { metrics.requests.Inc() },
				OnOffer:    func() { metrics.offers.Inc() },
				OnAccepted: func() { metrics.replies.WithLabelValues("accepted").Inc() },
				OnDeclined: func() { metrics.replies.WithLabelValues("declined").Inc() },
				OnError:    func(error) { metrics.pingErrors.Inc() },
				OnDuplicate: func(tripID string) {
					// Must stay at zero. A non-zero value means two drivers
					// held live offers for one rider, which is the exact
					// failure the single-writer sharding exists to prevent.
					metrics.duplicates.Inc()
					slog.Error("DOUBLE DISPATCH", "trip", tripID)
				},
			})
		if err != nil {
			return err
		}
		defer riders.Close()

		go func() { _ = riders.Run(ctx) }()
		slog.Info("demand started",
			"requestsPerSecond", riderSettings.RequestsPerSecond,
			"acceptRate", riderSettings.AcceptRate,
			"acceptLatency", riderSettings.AcceptLatency,
		)
	}

	errs := make(chan error, 2)
	go func() { errs <- registry.ServeMetrics(ctx, metricsAddr) }()
	go func() { errs <- serveControl(ctx, controlAddr, fleet, pool) }()

	select {
	case <-ctx.Done():
	case err := <-errs:
		if err != nil {
			return err
		}
	}

	slog.Info("draining")
	fleet.Stop()

	flush, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	return transport.Flush(flush)
}

type metrics struct {
	duplicates prometheus.Counter
	requests   prometheus.Counter
	offers     prometheus.Counter
	replies    *prometheus.CounterVec
	drivers    prometheus.Gauge
	pings      prometheus.Counter
	pingErrors prometheus.Counter
	arrivals   prometheus.Counter
	poolSize   prometheus.Gauge
	routeFetch *prometheus.HistogramVec
}

func newMetrics(registry *obs.Registry) *metrics {
	m := &metrics{
		duplicates: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "surge_sim_double_dispatch_total",
			Help: "Trips offered to a second driver while already accepted. Must be zero.",
		}),
		requests: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "surge_sim_requests_total", Help: "Ride requests generated.",
		}),
		offers: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "surge_sim_offers_received_total", Help: "Offers delivered to simulated drivers.",
		}),
		replies: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "surge_sim_offer_replies_total", Help: "Driver answers, by outcome.",
		}, []string{"outcome"}),
		drivers: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "surge_sim_drivers",
			Help: "Simulated drivers currently configured.",
		}),
		pings: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "surge_sim_pings_total",
			Help: "GPS pings emitted.",
		}),
		pingErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "surge_sim_ping_errors_total",
			Help: "Pings that failed to reach the transport.",
		}),
		arrivals: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "surge_sim_arrivals_total",
			Help: "Drivers reaching the end of a route and picking a new one.",
		}),
		poolSize: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "surge_sim_route_pool_size",
			Help: "Routes held in the shared corpus.",
		}),
		routeFetch: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "surge_sim_route_fetch_seconds",
			Help:    "Time to fetch one route from Valhalla.",
			Buckets: prometheus.DefBuckets,
		}, []string{"outcome"}),
	}

	registry.MustRegister(m.duplicates, m.requests, m.offers, m.replies,
		m.drivers, m.pings, m.pingErrors, m.arrivals, m.poolSize, m.routeFetch)
	return m
}

type configRequest struct {
	Drivers        *int     `json:"drivers"`
	PingIntervalMs *int     `json:"pingIntervalMs"`
	Seed           *uint64  `json:"seed"`
	SpeedKmhMin    *float64 `json:"speedKmhMin"`
	SpeedKmhMax    *float64 `json:"speedKmhMax"`
	GPSNoiseMeters *float64 `json:"gpsNoiseMeters"`
}

type statsResponse struct {
	Drivers        int     `json:"drivers"`
	Running        int     `json:"running"`
	PingsTotal     uint64  `json:"pingsTotal"`
	PingIntervalMs int64   `json:"pingIntervalMs"`
	TargetRate     float64 `json:"targetPingsPerSecond"`
	RoutePoolSize  int     `json:"routePoolSize"`
}

func serveControl(ctx context.Context, addr string, fleet *sim.Sim, pool *sim.RoutePool) error {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /sim/stats", func(w http.ResponseWriter, _ *http.Request) {
		settings := fleet.Config()
		writeJSON(w, http.StatusOK, statsResponse{
			Drivers:        settings.Drivers,
			Running:        fleet.Running(),
			PingsTotal:     fleet.Pings(),
			PingIntervalMs: settings.PingInterval.Milliseconds(),
			TargetRate:     float64(settings.Drivers) / settings.PingInterval.Seconds(),
			RoutePoolSize:  pool.Size(),
		})
	})

	mux.HandleFunc("POST /sim/config", func(w http.ResponseWriter, r *http.Request) {
		var request configRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		// Partial update: anything omitted keeps its current value, so dragging
		// the driver slider does not silently reset the ping interval.
		settings := fleet.Config()
		if request.Drivers != nil {
			settings.Drivers = *request.Drivers
		}
		if request.PingIntervalMs != nil {
			settings.PingInterval = time.Duration(*request.PingIntervalMs) * time.Millisecond
		}
		if request.Seed != nil {
			settings.Seed = *request.Seed
		}
		if request.SpeedKmhMin != nil {
			settings.SpeedKmhMin = *request.SpeedKmhMin
		}
		if request.SpeedKmhMax != nil {
			settings.SpeedKmhMax = *request.SpeedKmhMax
		}
		if request.GPSNoiseMeters != nil {
			settings.GPSNoiseMeters = *request.GPSNoiseMeters
		}

		// Scaled against the daemon's context, not the request's — otherwise
		// every driver started by this call would be cancelled the moment the
		// HTTP response was written.
		if err := fleet.Scale(ctx, settings); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		slog.Info("rescaled",
			"drivers", settings.Drivers,
			"pingInterval", settings.PingInterval,
			"pingsPerSecond", float64(settings.Drivers)/settings.PingInterval.Seconds(),
		)

		writeJSON(w, http.StatusOK, statsResponse{
			Drivers:        settings.Drivers,
			Running:        fleet.Running(),
			PingsTotal:     fleet.Pings(),
			PingIntervalMs: settings.PingInterval.Milliseconds(),
			TargetRate:     float64(settings.Drivers) / settings.PingInterval.Seconds(),
			RoutePoolSize:  pool.Size(),
		})
	})

	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()

	slog.Info("control api listening", "addr", addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("simd: control api: %w", err)
	}
	return nil
}

// floatOr mirrors config.IntOr for a float: unset takes the default, set but
// unparseable is an error.
func floatOr(key string, fallback float64) (float64, error) {
	raw := config.StringOr(key, "")
	if raw == "" {
		return fallback, nil
	}

	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("config: %s=%q is not a valid number: %w", key, raw, err)
	}
	return parsed, nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
