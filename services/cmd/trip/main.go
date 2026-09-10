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

	"github.com/ishakdeveloper/surge/internal/trip/domain"
	"github.com/ishakdeveloper/surge/internal/trip/infrastructure/events"
	triphandler "github.com/ishakdeveloper/surge/internal/trip/infrastructure/grpc"
	"github.com/ishakdeveloper/surge/internal/trip/infrastructure/repository"
	triprouting "github.com/ishakdeveloper/surge/internal/trip/infrastructure/routing"
	"github.com/ishakdeveloper/surge/internal/trip/service"
	"github.com/ishakdeveloper/surge/pkg/config"
	"github.com/ishakdeveloper/surge/pkg/kafkax"
	"github.com/ishakdeveloper/surge/pkg/obs"
	trippb "github.com/ishakdeveloper/surge/pkg/proto/trip"
	"github.com/ishakdeveloper/surge/pkg/routing"
	"github.com/ishakdeveloper/surge/pkg/tracing"
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
	grpcAddr := config.StringOr("TRIP_GRPC_ADDR", ":8110")
	metricsAddr := config.StringOr("TRIP_METRICS_ADDR", ":9105")
	valhallaURL := config.StringOr("VALHALLA_URL", "http://localhost:8002")
	group := config.StringOr("TRIP_GROUP", "trip")

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
	if err := router.Healthy(ctx); err != nil {
		return fmt.Errorf("trip: %w", err)
	}

	// Postgres when configured, memory otherwise.
	//
	// Not a convenience: it means the service runs on a laptop with nothing but
	// a broker, and it is the same seam the tests use. A repository behind an
	// interface is only worth having if something other than Postgres actually
	// implements it.
	repo, closeRepo, err := openRepository(ctx)
	if err != nil {
		return err
	}
	defer closeRepo()

	producer, err := kafkax.NewProducer(brokers)
	if err != nil {
		return err
	}
	defer producer.Close()

	fares := repository.NewFareCache()

	trip := service.New(service.Options{
		Trips:   repo,
		Fares:   fares,
		Router:  triprouting.NewValhalla(router),
		Surge:   triprouting.FlatSurge{},
		Matcher: events.NewMatchRequester(producer),
	})

	consumer, err := events.NewConsumer(brokers, group, trip, events.Hooks{
		OnMatched:   func() { metrics.outcomes.WithLabelValues("matched").Inc() },
		OnUnmatched: func() { metrics.outcomes.WithLabelValues("unmatched").Inc() },
		OnRejected:  func(reason string) { metrics.rejected.WithLabelValues(reason).Inc() },
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

	errs := make(chan error, 3)
	go func() { errs <- registry.ServeMetrics(ctx, metricsAddr) }()
	go func() { errs <- consumer.Run(ctx) }()
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

func openRepository(ctx context.Context) (domain.Repository, func(), error) {
	url := config.StringOr("DATABASE_URL", "")
	if url == "" {
		slog.Warn("DATABASE_URL is not set; trips are held in memory and lost on restart")
		return repository.NewInMemory(), func() {}, nil
	}

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, nil, fmt.Errorf("trip: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("trip: ping: %w", err)
	}

	slog.Info("trips are durable", "database", "postgres")
	return repository.NewPostgres(pool), pool.Close, nil
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
