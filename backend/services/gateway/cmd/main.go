// Command gateway is the edge.
//
// The only process browsers and phones talk to, and the only one that is
// stateful about *who* is connected rather than about what is happening. Riders
// call it over REST and it fans out to internal services over gRPC; drivers
// hold a WebSocket and it turns their frames into records on the bus.
//
// It scales on connection count, which is why it is its own service: nothing
// else here cares how many sockets are open, and this cares about little else.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ishakdeveloper/surge/services/gateway/internal/domain"
	"github.com/ishakdeveloper/surge/services/gateway/internal/infrastructure/events"
	gatewaygrpc "github.com/ishakdeveloper/surge/services/gateway/internal/infrastructure/grpc"
	gatewayhttp "github.com/ishakdeveloper/surge/services/gateway/internal/infrastructure/http"
	"github.com/ishakdeveloper/surge/services/gateway/internal/infrastructure/ws"
	"github.com/ishakdeveloper/surge/shared/authz"
	"github.com/ishakdeveloper/surge/shared/config"
	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/ishakdeveloper/surge/shared/obs"
	"github.com/ishakdeveloper/surge/shared/retry"
	"github.com/ishakdeveloper/surge/shared/tracing"
	"github.com/prometheus/client_golang/prometheus"
)

// produceSample rate-limits produce-failure logging.
var produceSample atomic.Uint64

func main() {
	if err := run(); err != nil {
		slog.Error("gateway exited", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var (
		brokers     = config.Strings("KAFKA_BROKERS", []string{"localhost:19092"})
		httpAddr    = config.StringOr("GATEWAY_HTTP_ADDR", ":8100")
		metricsAddr = config.StringOr("GATEWAY_METRICS_ADDR", ":9104")
		tripAddr    = config.StringOr("TRIP_GRPC_ADDR", "localhost:8110")
		simAddr     = config.StringOr("SIM_GRPC_ADDR", "localhost:8111")
		webOrigins  = config.Strings("GATEWAY_ORIGINS", []string{"http://localhost:5273"})
	)

	shutdownTracing, err := tracing.Init(ctx, "gateway",
		config.StringOr("OTEL_EXPORTER_OTLP_ENDPOINT", ""))
	if err != nil {
		return err
	}
	defer func() {
		flush, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = shutdownTracing(flush)
	}()

	verifier, err := buildVerifier(ctx)
	if err != nil {
		return err
	}

	if err := kafkax.EnsureTopics(ctx, brokers); err != nil {
		return err
	}

	registry := obs.NewRegistry("gateway")
	metrics := newMetrics(registry)

	producer, err := kafkax.NewProducer(brokers)
	if err != nil {
		return err
	}
	defer producer.Close()

	clients, err := gatewaygrpc.Dial(tripAddr, simAddr)
	if err != nil {
		return err
	}
	defer clients.Close()

	connections := domain.NewRegistry()
	fleet := domain.NewFleet()

	hub := ws.NewHub(connections, verifier, producer, webOrigins, fleet, clients.Trip, ws.Hooks{
		OnConnect: func(role string) { metrics.connects.WithLabelValues(role).Inc() },
		OnDisconnect: func(role string, reason domain.EvictionReason) {
			metrics.disconnects.WithLabelValues(role, string(reason)).Inc()
		},
		OnInbound:    func(tag string) { metrics.inbound.WithLabelValues(tag).Inc() },
		OnRejected:   func(reason string) { metrics.rejected.WithLabelValues(reason).Inc() },
		OnPushed:     func() { metrics.pushed.WithLabelValues("delivered").Inc() },
		OnPushFailed: func(reason string) { metrics.pushed.WithLabelValues(reason).Inc() },
		OnProduceError: func(topic string, err error) {
			// Sampled: one line per failed record would be its own outage.
			if produceSample.Add(1)%200 == 1 {
				slog.Error("produce failed", "topic", topic, "error", err)
			}
		},
	})

	// Its own consumer group per instance: every gateway needs every offer,
	// because only the instance holding that driver's socket can deliver it.
	instance := config.StringOr("HOSTNAME", fmt.Sprintf("gateway-%d", os.Getpid()))
	pushes, err := events.NewConsumer(brokers, "gateway-push-"+instance, hub, events.Hooks{
		OnDelivered: func() { metrics.pushRouted.WithLabelValues("delivered").Inc() },
		OnNotHere:   func() { metrics.pushRouted.WithLabelValues("not_here").Inc() },
		OnMalformed: func() { metrics.pushRouted.WithLabelValues("malformed").Inc() },
	})
	if err != nil {
		return err
	}
	defer pushes.Close()

	// The matchers' frames, which every instance needs for the same reason:
	// a console may be connected to any of them.
	frames, err := events.NewFleetConsumer(brokers, "gateway-fleet-"+instance, fleet)
	if err != nil {
		return err
	}
	defer frames.Close()

	guard := authz.Middleware(verifier, func(reason string) {
		metrics.rejected.WithLabelValues(reason).Inc()
	})

	// The REST surface, generated from the proto annotations.
	rest, err := gatewayhttp.NewMux(ctx, clients.TripConn(), clients.SimulatorConn())
	if err != nil {
		return err
	}

	mux := http.NewServeMux()

	// Behind the auth guard, which is what puts a verified caller in the
	// request context for the metadata annotator to forward. Everything under
	// /v1 needs a caller; nothing under it is public.
	mux.Handle("/v1/", guard(rest))

	// The API document, generated from the same annotations. Served rather
	// than published separately, so what a client reads and what the gateway
	// does cannot disagree.
	mux.Handle("GET /openapi.json", gatewayhttp.OpenAPI())

	// Unauthenticated on purpose: an orchestrator probing liveness has no
	// token, and requiring one would make the gateway look dead to it.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ready"))
	})

	// The socket authenticates itself before upgrading, so it is not behind the
	// HTTP guard — a 401 body is not something a WebSocket client can read.
	mux.Handle("/ws", hub.Handler())

	server := &http.Server{
		Addr:              httpAddr,
		Handler:           gatewayhttp.CORS(webOrigins)(mux),
		ReadHeaderTimeout: 10 * time.Second,
		// No WriteTimeout: it would apply to the WebSocket too, and cut every
		// connection at the deadline regardless of health.
		IdleTimeout: 120 * time.Second,
	}

	// Labelled, because a bare error channel makes every failure look the same.
	// A gateway that stopped listening because its metrics port was taken looks
	// exactly like one that crashed, right up until you read the code.
	type failure struct {
		subsystem string
		err       error
	}
	errs := make(chan failure, 4)

	go func() { errs <- failure{"metrics", registry.ServeMetrics(ctx, metricsAddr)} }()
	go func() { errs <- failure{"push consumer", pushes.Run(ctx)} }()
	go func() { errs <- failure{"fleet consumer", frames.Run(ctx)} }()
	go hub.RunFleet(ctx)
	go func() {
		slog.Info("gateway listening", "addr", httpAddr, "trip", tripAddr, "origins", webOrigins)
		errs <- failure{"http", server.ListenAndServe()}
	}()

	go func() {
		sample := time.NewTicker(2 * time.Second)
		defer sample.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-sample.C:
				metrics.connections.Set(float64(connections.Len()))
				// The early warning. The average send-queue depth stays near
				// zero right up until something breaks; the deepest one moves
				// first.
				metrics.backlog.Set(float64(connections.Backlog()))
				if evicted := connections.EvictIdle(time.Now().Add(-3 * time.Minute)); evicted > 0 {
					metrics.disconnects.WithLabelValues("unknown", string(domain.EvictionIdle)).Add(float64(evicted))
				}
			}
		}
	}()

	var fatal error

	select {
	case <-ctx.Done():
		slog.Info("shutting down on signal")
	case f := <-errs:
		switch {
		case f.err == nil:
			slog.Warn("subsystem stopped", "subsystem", f.subsystem)
		case errors.Is(f.err, http.ErrServerClosed):
		default:
			slog.Error("subsystem failed", "subsystem", f.subsystem, "error", f.err)
			fatal = f.err
		}
	}

	// Connections first, listener second.
	//
	// http.Server.Shutdown waits for active handlers, and a WebSocket handler
	// only returns when its connection closes. Shutting down without this hangs
	// forever: the listener is gone, so nothing can reach the process, and the
	// process will not exit.
	if closed := connections.CloseAll(domain.EvictionShutdown); closed > 0 {
		slog.Info("closed connections for shutdown", "count", closed)
	}

	shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdown); err != nil {
		// Something is still holding a handler open past the grace period.
		// Close is abrupt, and abrupt beats never.
		slog.Warn("graceful shutdown timed out, closing", "error", err)
		_ = server.Close()
	}

	return fatal
}

// buildVerifier assembles who this gateway will believe.
func buildVerifier(ctx context.Context) (authz.Verifier, error) {
	jwksURL := config.StringOr("AUTH_JWKS_URL", "http://localhost:3200/api/auth/jwks")
	issuer := config.StringOr("AUTH_ISSUER", "http://localhost:3200")
	audience := config.StringOr("AUTH_AUDIENCE", "surge")

	// Retried, not fatal on the first refusal. The auth service is a separate
	// deployment with its own rollout, and a gateway that exits because
	// authentication was thirty seconds behind it turns an ordinary startup
	// ordering into a CrashLoopBackOff whose backoff grows to five minutes.
	var users *authz.JWKS

	err := retry.Do(ctx, retry.Config{
		Attempts: 40,
		Initial:  2 * time.Second,
		Max:      15 * time.Second,
		Jitter:   true,
	}, func(ctx context.Context) error {
		built, err := authz.NewJWKS(ctx, jwksURL, issuer, audience)
		if err != nil {
			slog.Info("waiting for the auth service", "jwks", jwksURL)
			return err
		}
		users = built
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("auth service never became reachable at %s: %w", jwksURL, err)
	}
	chain := authz.Chain{users}

	simEnabled, err := config.BoolOr("SIM_ENABLED", false)
	if err != nil {
		return nil, err
	}
	if !simEnabled {
		return chain, nil
	}

	secret, err := config.String("SIM_TOKEN_SECRET")
	if err != nil {
		return nil, fmt.Errorf("gateway: SIM_ENABLED is set but %w", err)
	}

	sim, err := authz.NewHMAC(secret, "surge-sim", audience)
	if err != nil {
		return nil, err
	}

	// Loud, because this is a gateway that will accept self-minted driver
	// identities. It should never be a surprise in a log.
	slog.Warn("simulator tokens are accepted; this must not be set in a real deployment")
	return append(chain, sim), nil
}

type metrics struct {
	connections prometheus.Gauge
	backlog     prometheus.Gauge
	connects    *prometheus.CounterVec
	disconnects *prometheus.CounterVec
	inbound     *prometheus.CounterVec
	rejected    *prometheus.CounterVec
	pushed      *prometheus.CounterVec
	pushRouted  *prometheus.CounterVec
}

func newMetrics(registry *obs.Registry) *metrics {
	m := &metrics{
		connections: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "surge_gateway_connections", Help: "Open WebSocket connections on this instance.",
		}),
		backlog: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "surge_gateway_max_send_backlog",
			Help: "Deepest per-connection send queue. Moves before anything is dropped.",
		}),
		connects: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "surge_gateway_connects_total", Help: "Connections accepted, by role.",
		}, []string{"role"}),
		disconnects: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "surge_gateway_disconnects_total",
			Help: "Connections closed, by role and reason. `slow_consumer` is backpressure working.",
		}, []string{"role", "reason"}),
		inbound: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "surge_gateway_inbound_total", Help: "Client messages received, by tag.",
		}, []string{"tag"}),
		rejected: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "surge_gateway_rejected_total", Help: "Requests refused, by reason.",
		}, []string{"reason"}),
		pushed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "surge_gateway_push_total", Help: "Push attempts to a connection, by outcome.",
		}, []string{"outcome"}),
		pushRouted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "surge_gateway_push_routed_total",
			Help: "Offers consumed from ws.push. A rising not_here:delivered ratio says per-instance topics are becoming worth it.",
		}, []string{"outcome"}),
	}

	registry.MustRegister(m.connections, m.backlog, m.connects, m.disconnects,
		m.inbound, m.rejected, m.pushed, m.pushRouted)
	return m
}
