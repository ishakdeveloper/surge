// Command trip owns the trip lifecycle.
//
// The service with two faces, and the split is deliberate. Riders ask it things
// synchronously over gRPC — preview a fare, book it, cancel — because a person
// is waiting for an answer. It learns what happened asynchronously from
// `trip.events`, because matching takes as long as a driver takes to answer and
// nobody should hold an open RPC for that.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ishakdeveloper/surge/services/trip/internal/domain"
	"github.com/ishakdeveloper/surge/services/trip/internal/infrastructure/events"
	triphandler "github.com/ishakdeveloper/surge/services/trip/internal/infrastructure/grpc"
	"github.com/ishakdeveloper/surge/services/trip/internal/infrastructure/repository"
	triprouting "github.com/ishakdeveloper/surge/services/trip/internal/infrastructure/routing"
	"github.com/ishakdeveloper/surge/services/trip/internal/service"
	"github.com/ishakdeveloper/surge/shared/authz"
	"github.com/ishakdeveloper/surge/shared/config"
	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/ishakdeveloper/surge/shared/obs"
	"github.com/ishakdeveloper/surge/shared/outbox"
	trippb "github.com/ishakdeveloper/surge/shared/proto/trip"
	"github.com/ishakdeveloper/surge/shared/retry"
	"github.com/ishakdeveloper/surge/shared/routing"
	"github.com/ishakdeveloper/surge/shared/tracing"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/stats/opentelemetry"
)

func main() {
	if err := run(); err != nil {
		slog.Error("trip exited", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	brokers := config.Strings("KAFKA_BROKERS", []string{"localhost:19092"})
	// Listen address, NOT the address clients dial.
	//
	// These were one variable until a deploy proved they are two: a pod binds
	// ":8110" on its own interface, while a caller dials "trip:8110", the
	// Service name. Sharing the variable worked locally only because
	// "localhost:8110" happens to be a valid answer to both questions, and
	// failed in the cluster with "cannot assign requested address" — the pod
	// trying to bind an IP that belongs to the Service.
	grpcAddr := config.StringOr("TRIP_GRPC_LISTEN", ":8110")
	metricsAddr := config.StringOr("TRIP_METRICS_ADDR", ":9105")
	valhallaURL := config.StringOr("VALHALLA_URL", "http://localhost:8002")
	group := config.StringOr("TRIP_GROUP", "trip")
	requirePayment, err := config.BoolOr("TRIP_REQUIRE_PAYMENT", false)
	if err != nil {
		return err
	}

	shutdownTracing, err := tracing.Init(ctx, "trip",
		config.StringOr("OTEL_EXPORTER_OTLP_ENDPOINT", ""))
	if err != nil {
		return err
	}
	defer func() {
		flush, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = shutdownTracing(flush)
	}()

	registry := obs.NewRegistry("trip")
	metrics := newMetrics(registry)

	if err := kafkax.EnsureTopics(ctx, brokers); err != nil {
		return err
	}

	router := routing.New(valhallaURL)
	if err := waitForRouting(ctx, router); err != nil {
		return fmt.Errorf("trip: %w", err)
	}

	// Postgres when configured, memory otherwise.
	//
	// Not a convenience: it means the service runs on a laptop with nothing but
	// a broker, and it is the same seam the tests use. A repository behind an
	// interface is only worth having if something other than Postgres actually
	// implements it.
	repo, pool, closeRepo, err := openRepository(ctx)
	if err != nil {
		return err
	}
	defer closeRepo()

	producer, err := kafkax.NewProducer(brokers)
	if err != nil {
		return err
	}
	defer producer.Close()

	// The relay is what turns committed facts into `trip.lifecycle` records.
	// Only with Postgres: the in-memory repository has no outbox to relay, and
	// nothing that consumes the topic runs without a database either.
	var relay *outbox.Relay
	if pool != nil {
		relay = outbox.NewRelay(pool, repository.OutboxTable, producer, outbox.Options{
			Hooks: outbox.PrometheusHooks(registry, "trip"),
		})
	}

	fares := repository.NewFareCache()

	// The matchers publish each cell's multiplier; a quote reads its pickup's.
	surge, err := events.NewSurgeBook(brokers)
	if err != nil {
		return err
	}
	defer surge.Close()

	trip := service.New(service.Options{
		Trips:   repo,
		Fares:   fares,
		Router:  triprouting.NewValhalla(router),
		Surge:   surge,
		Matcher: events.NewMatchRequester(producer),
		// Every transition is pushed to the rider and driver on it, which is
		// what lets a rider's screen change the moment a driver accepts.
		Notifier:       events.NewTripNotifier(producer),
		RequirePayment: requirePayment,
	})
	if requirePayment {
		slog.Info("bookings wait for payments to hold the fare before dispatch")
	}

	consumer, err := events.NewConsumer(brokers, group, trip, events.Hooks{
		OnMatched:   func() { metrics.outcomes.WithLabelValues("matched").Inc() },
		OnUnmatched: func() { metrics.outcomes.WithLabelValues("unmatched").Inc() },
		OnRejected:  func(reason string) { metrics.rejected.WithLabelValues(reason).Inc() },
		OnPayment:   func(kind string) { metrics.outcomes.WithLabelValues("payment_" + kind).Inc() },
	})
	if err != nil {
		return err
	}
	defer consumer.Close()

	server := grpc.NewServer(
		// Server-side tracing and metrics from the same interceptor the
		// ecosystem standardises on, so a gRPC call joins the trace that a
		// Kafka record started rather than beginning a new one.
		opentelemetry.ServerOption(opentelemetry.Options{}),
		// Turns the metadata the gateway forwarded back into an Identity, so a
		// handler reads the caller from context exactly as an HTTP handler
		// does and never thinks about metadata at all.
		grpc.UnaryInterceptor(authz.UnaryServerInterceptor()),
	)
	trippb.RegisterTripServiceServer(server, triphandler.NewHandler(trip))

	// Health and reflection: the first is what an orchestrator probes, the
	// second is what makes `grpcurl` work without a copy of the protos.
	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(server, healthServer)
	healthServer.SetServingStatus("surge.trip.v1.TripService", healthpb.HealthCheckResponse_SERVING)
	reflection.Register(server)

	listener, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		return fmt.Errorf("trip: listen %s: %w", grpcAddr, err)
	}

	// One slot per sender, so none blocks once the first error has been read.
	errs := make(chan error, 5)
	go func() { errs <- registry.ServeMetrics(ctx, metricsAddr) }()
	go func() { errs <- consumer.Run(ctx) }()
	if relay != nil {
		go func() { errs <- relay.Run(ctx) }()
	}
	go func() { errs <- surge.Run(ctx) }()
	go func() {
		slog.Info("grpc listening", "addr", grpcAddr)
		errs <- server.Serve(listener)
	}()

	// Abandoned quotes are a slow leak: a rider dragging a pin makes three per
	// movement and never accepts most of them.
	go func() {
		sweep := time.NewTicker(time.Minute)
		defer sweep.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-sweep.C:
				metrics.faresHeld.Set(float64(fares.Len()))
				if removed := fares.Sweep(); removed > 0 {
					metrics.faresSwept.Add(float64(removed))
				}
			}
		}
	}()

	select {
	case <-ctx.Done():
		server.GracefulStop()
		return nil
	case err := <-errs:
		server.GracefulStop()
		if errors.Is(err, grpc.ErrServerStopped) {
			return nil
		}
		return err
	}
}

// openRepository returns the pool alongside the repository, nil when there is
// none, because the outbox relay needs the database the repository writes to.
func openRepository(ctx context.Context) (domain.Repository, *pgxpool.Pool, func(), error) {
	url := config.StringOr("DATABASE_URL", "")
	if url == "" {
		slog.Warn("DATABASE_URL is not set; trips are held in memory, lost on restart, " +
			"and their lifecycle facts are never published")
		return repository.NewInMemory(), nil, func() {}, nil
	}

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("trip: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, nil, nil, fmt.Errorf("trip: ping: %w", err)
	}

	slog.Info("trips are durable", "database", "postgres")
	return repository.NewPostgres(pool), pool, pool.Close, nil
}

type metrics struct {
	outcomes   *prometheus.CounterVec
	rejected   *prometheus.CounterVec
	faresHeld  prometheus.Gauge
	faresSwept prometheus.Counter
}

func newMetrics(registry *obs.Registry) *metrics {
	m := &metrics{
		outcomes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "surge_trip_outcomes_total", Help: "Trip outcomes applied, by kind.",
		}, []string{"kind"}),
		rejected: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "surge_trip_events_rejected_total",
			Help: "Trip events that did not apply. `invalid_transition` is the state machine working.",
		}, []string{"reason"}),
		faresHeld: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "surge_trip_fares_held", Help: "Quotes held in memory awaiting a booking.",
		}),
		faresSwept: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "surge_trip_fares_swept_total", Help: "Expired quotes evicted.",
		}),
	}
	registry.MustRegister(m.outcomes, m.rejected, m.faresHeld, m.faresSwept)
	return m
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
