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
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ishakdeveloper/surge/services/simulator/internal/control"
	"github.com/ishakdeveloper/surge/services/simulator/internal/sim"
	"github.com/ishakdeveloper/surge/shared/authz"
	"github.com/ishakdeveloper/surge/shared/config"
	"github.com/ishakdeveloper/surge/shared/geo"
	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/ishakdeveloper/surge/shared/obs"
	simpb "github.com/ishakdeveloper/surge/shared/proto/sim"
	"github.com/ishakdeveloper/surge/shared/retry"
	"github.com/ishakdeveloper/surge/shared/routing"
	"github.com/ishakdeveloper/surge/shared/tracing"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
)

// disconnectSample rate-limits disconnect logging.
var disconnectSample atomic.Uint64

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
	// The same knob over gRPC, for the gateway to put behind /v1/simulator.
	// The HTTP one stays for `make load` and curl, which have no token.
	grpcAddr := config.StringOr("SIM_GRPC_LISTEN", ":8111")
	metricsAddr := config.StringOr("SIM_METRICS_ADDR", ":9101")

	poolTarget, err := config.IntOr("SIM_ROUTE_POOL", 400)
	if err != nil {
		return err
	}
	poolConcurrency, err := config.IntOr("SIM_ROUTE_CONCURRENCY", 8)
	if err != nil {
		return err
	}
	// kafka | ws | discard.
	//
	// `kafka` produces straight to the bus, which is what Phase 1 measured and
	// is still the way to isolate ingest from the gateway. `ws` opens a real
	// connection per driver through the gateway, which is the only way to
	// exercise the connection registry and slow-consumer eviction. `discard` is
	// the control: it measures what the simulator itself costs.
	transportKind := config.StringOr("SIM_TRANSPORT", "kafka")
	if legacy, err := config.BoolOr("SIM_KAFKA", true); err != nil {
		return err
	} else if !legacy && transportKind == "kafka" {
		transportKind = "discard"
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
	if riderSettings.HotspotShare, err = floatOr("SIM_HOTSPOT_SHARE", riderSettings.HotspotShare); err != nil {
		return err
	}

	settings := sim.DefaultConfig()
	if settings.Drivers, err = config.IntOr("SIM_DRIVERS", settings.Drivers); err != nil {
		return err
	}
	if settings.PingInterval, err = config.DurationOr("SIM_PING_INTERVAL", settings.PingInterval); err != nil {
		return err
	}
	// Trips: an accepted driver drives to the pickup and carries the rider
	// about this far. Zero is the fleet that never gets busy.
	if settings.TripKm, err = floatOr("SIM_TRIP_KM", settings.TripKm); err != nil {
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
	if err := waitForRouting(ctx, router); err != nil {
		return fmt.Errorf("simd: %w", err)
	}

	riderSettings.Seed = settings.Seed
	riderSettings.Run = settings.Epoch
	policy := sim.NewOfferPolicy(riderSettings)

	var (
		transport sim.Transport
		sockets   *sim.WSTransport
	)

	switch transportKind {
	case "ws":
		if err := kafkax.EnsureTopics(ctx, brokers); err != nil {
			return err
		}

		secret, err := config.String("SIM_TOKEN_SECRET")
		if err != nil {
			return fmt.Errorf("simd: SIM_TRANSPORT=ws needs %w", err)
		}
		dialConcurrency, err := config.IntOr("SIM_DIAL_CONCURRENCY", 64)
		if err != nil {
			return err
		}

		sockets, err = sim.NewWSTransport(
			config.StringOr("SIM_WS_URL", "ws://localhost:8100/ws"),
			secret,
			config.StringOr("AUTH_AUDIENCE", "surge"),
			policy,
			dialConcurrency,
			sim.WSHooks{
				OnConnected: func() { metrics.wsConnections.Inc() },
				OnDisconnected: func(reason string, cause error) {
					metrics.wsDisconnects.WithLabelValues(reason).Inc()
					// Sampled: at this scale a log line per disconnect is its
					// own outage, but the first few carry the actual error,
					// which a label never does.
					if cause != nil && disconnectSample.Add(1)%500 == 1 {
						slog.Warn("driver socket lost", "reason", reason, "error", cause)
					}
				},
				OnDialFailed: func() { metrics.wsDialFailures.Inc() },
				OnOffer:      func() { metrics.offers.Inc() },
				OnReply: func(accepted bool) {
					outcome := "declined"
					if accepted {
						outcome = "accepted"
					}
					metrics.replies.WithLabelValues(outcome).Inc()
				},
				OnDuplicate: func(tripID string) {
					metrics.duplicates.Inc()
					slog.Error("DOUBLE DISPATCH", "trip", tripID)
				},
			})
		if err != nil {
			return err
		}
		transport = sockets
		slog.Info("drivers connect over websockets", "url", config.StringOr("SIM_WS_URL", "ws://localhost:8100/ws"))

	case "discard":
		// The control run: measures what the simulator itself costs, so a
		// throughput ceiling can be attributed rather than guessed at. Counting
		// is left to the fleet hook below — doing it here as well would double
		// every ping and make the control run look twice as fast as it is.
		transport = sim.NewDiscardTransport(nil)
		slog.Warn("pings are discarded; this measures the simulator only")

	default:
		if err := kafkax.EnsureTopics(ctx, brokers); err != nil {
			return err
		}
		kafka, err := sim.NewKafkaTransport(brokers, func(error) { metrics.pingErrors.Inc() })
		if err != nil {
			return err
		}
		transport = kafka
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

	// Whichever transport answers an offer, the driver who accepted it does the
	// trip.
	policy.OnCommitted(fleet.Assign)

	if err := fleet.Scale(ctx, settings); err != nil {
		return err
	}
	slog.Info("fleet started",
		"drivers", settings.Drivers,
		"tripKm", settings.TripKm,
		"hotspotShare", riderSettings.HotspotShare,
		"pingInterval", settings.PingInterval,
		"pingsPerSecond", float64(settings.Drivers)/settings.PingInterval.Seconds(),
	)

	// Demand, and the drivers who answer it. Only with Kafka: the discard
	// transport is a control run measuring the simulator itself, and there is
	// nothing on the other end to match against.
	if transportKind != "discard" {
		riders, err := sim.NewRiders(brokers, config.StringOr("SIM_RIDER_GROUP", "sim-drivers"), pool, riderSettings, policy,
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

		// Only the Kafka transport needs the rider half to play drivers too.
		answerOffers := transportKind == "kafka"
		go func() { _ = riders.Run(ctx, answerOffers) }()
		slog.Info("demand started",
			"transport", transportKind,
			"driversAnswerOverKafka", answerOffers,
			"requestsPerSecond", riderSettings.RequestsPerSecond,
			"acceptRate", riderSettings.AcceptRate,
			"acceptLatency", riderSettings.AcceptLatency,
		)
	}

	server := grpc.NewServer(grpc.UnaryInterceptor(authz.UnaryServerInterceptor()))
	simpb.RegisterSimulatorServiceServer(server, control.NewServer(ctx, fleet))
	listener, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		return fmt.Errorf("simd: listen %s: %w", grpcAddr, err)
	}
	defer server.GracefulStop()

	errs := make(chan error, 3)
	go func() { errs <- registry.ServeMetrics(ctx, metricsAddr) }()
	go func() { errs <- serveControl(ctx, controlAddr, fleet, pool) }()
	go func() {
		slog.Info("control grpc listening", "addr", grpcAddr)
		errs <- server.Serve(listener)
	}()

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
	wsConnections  prometheus.Counter
	wsDisconnects  *prometheus.CounterVec
	wsDialFailures prometheus.Counter
	duplicates     prometheus.Counter
	requests       prometheus.Counter
	offers         prometheus.Counter
	replies        *prometheus.CounterVec
	drivers        prometheus.Gauge
	pings          prometheus.Counter
	pingErrors     prometheus.Counter
	arrivals       prometheus.Counter
	poolSize       prometheus.Gauge
	routeFetch     *prometheus.HistogramVec
}

func newMetrics(registry *obs.Registry) *metrics {
	m := &metrics{
		wsConnections: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "surge_sim_ws_connected_total", Help: "WebSocket connections opened by simulated drivers.",
		}),
		wsDisconnects: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "surge_sim_ws_disconnects_total",
			Help: "Simulated driver connections lost, by reason. Should stay near zero; a spike means the gateway is shedding.",
		}, []string{"reason"}),
		wsDialFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "surge_sim_ws_dial_failures_total", Help: "Connections the simulator could not establish.",
		}),
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

	registry.MustRegister(m.wsConnections, m.wsDisconnects, m.wsDialFailures,
		m.duplicates, m.requests, m.offers, m.replies,
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

// waitForRouting blocks until Valhalla is serving, or gives up.
//
// Rather than exiting on the first refused connection. In Kubernetes that would
// be a CrashLoopBackOff, which does eventually converge — but its backoff grows
// to five minutes, so a dependency that took twenty minutes to become ready
// leaves the service down for well past that. Valhalla builds tiles from a
// 189MB extract on first boot, so this is the normal case rather than the
// exceptional one.
//
// It still gives up. A service that waits forever on a dependency that is never
// coming is indistinguishable from one that is broken, and at least a crash is
// visible.
func waitForRouting(ctx context.Context, router *routing.Client) error {
	var attempts int

	err := retry.Do(ctx, retry.Config{
		Attempts: 60,
		Initial:  2 * time.Second,
		Max:      15 * time.Second,
		Jitter:   true,
	}, func(ctx context.Context) error {
		attempts++
		if err := router.Healthy(ctx); err != nil {
			if attempts == 1 || attempts%10 == 0 {
				slog.Info("waiting for the routing engine", "attempt", attempts, "error", err)
			}
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("routing engine never became ready: %w", err)
	}

	slog.Info("routing engine ready", "attempts", attempts)
	return nil
}
