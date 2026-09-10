// Command payments owns money: the hold placed on a rider's card when a trip
// is requested, the capture when it completes, the release when it does not,
// and the driver's share passed on.
//
// Separate for isolation rather than scale. It is the only process holding
// processor credentials and the only one calling an external money API, whose
// latency and outages must never stall the trip state machine — so the trip
// service never calls it. Trip publishes facts through an outbox; this acts on
// them and answers the same way.
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

	"github.com/ishakdeveloper/surge/services/payments/internal/infrastructure/events"
	"github.com/ishakdeveloper/surge/services/payments/internal/infrastructure/fake"
	paymentshandler "github.com/ishakdeveloper/surge/services/payments/internal/infrastructure/grpc"
	"github.com/ishakdeveloper/surge/services/payments/internal/infrastructure/repository"
	"github.com/ishakdeveloper/surge/services/payments/internal/service"
	"github.com/ishakdeveloper/surge/shared/authz"
	"github.com/ishakdeveloper/surge/shared/config"
	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/ishakdeveloper/surge/shared/obs"
	"github.com/ishakdeveloper/surge/shared/outbox"
	paymentspb "github.com/ishakdeveloper/surge/shared/proto/payments"
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
		slog.Error("payments exited", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	brokers := config.Strings("KAFKA_BROKERS", []string{"localhost:19092"})
	metricsAddr := config.StringOr("PAYMENTS_METRICS_ADDR", ":9107")
	// Listen address, not the address the gateway dials: the same split the
	// trip service learned the hard way.
	grpcAddr := config.StringOr("PAYMENTS_GRPC_LISTEN", ":8112")
	group := config.StringOr("PAYMENTS_GROUP", "payments")
	webURL := config.StringOr("PAYMENTS_WEB_URL", "http://localhost:5273")

	commission, err := config.IntOr("PAYMENTS_COMMISSION_BPS", 2000)
	if err != nil {
		return err
	}

	// Required, with no in-memory fallback. The trip service can run from a
	// map on a laptop; a payments service that forgets on restart which cards
	// it holds money on cannot.
	databaseURL, err := config.String("DATABASE_URL")
	if err != nil {
		return err
	}

	processor, err := openProcessor()
	if err != nil {
		return err
	}

	shutdownTracing, err := tracing.Init(ctx, "payments",
		config.StringOr("OTEL_EXPORTER_OTLP_ENDPOINT", ""))
	if err != nil {
		return err
	}
	defer func() {
		flush, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = shutdownTracing(flush)
	}()

	registry := obs.NewRegistry("payments")
	metrics := newMetrics(registry)

	if err := kafkax.EnsureTopics(ctx, brokers); err != nil {
		return err
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("payments: connect: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("payments: ping: %w", err)
	}

	repo := repository.NewPostgres(pool)
	payments, err := service.New(service.Options{
		Repository:    repo,
		Processor:     processor,
		CommissionBps: commission,
		WebURL:        webURL,
	})
	if err != nil {
		return err
	}

	producer, err := kafkax.NewProducer(brokers)
	if err != nil {
		return err
	}
	defer producer.Close()

	relay := outbox.NewRelay(pool, repository.OutboxTable, producer, outbox.Options{
		Hooks: outbox.PrometheusHooks(registry, "payments"),
	})

	consumer, err := events.NewConsumer(brokers, group, payments, events.Hooks{
		OnApplied: func(tag string) { metrics.applied.WithLabelValues(tag).Inc() },
		OnSkipped: func(reason string) { metrics.skipped.WithLabelValues(reason).Inc() },
		OnRetry:   func() { metrics.retries.Inc() },
	})
	if err != nil {
		return err
	}
	defer consumer.Close()

	server := grpc.NewServer(
		opentelemetry.ServerOption(opentelemetry.Options{}),
		// The caller the gateway verified, back out of metadata, so a handler
		// reads who is asking from context and never from the request.
		grpc.UnaryInterceptor(authz.UnaryServerInterceptor()),
	)
	paymentspb.RegisterPaymentsServiceServer(server, paymentshandler.NewHandler(payments))
	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(server, healthServer)
	healthServer.SetServingStatus("surge.payments.v1.PaymentsService", healthpb.HealthCheckResponse_SERVING)
	reflection.Register(server)

	listener, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		return fmt.Errorf("payments: listen %s: %w", grpcAddr, err)
	}

	errs := make(chan error, 4)
	go func() { errs <- registry.ServeMetrics(ctx, metricsAddr) }()
	go func() { errs <- relay.Run(ctx) }()
	go func() { errs <- consumer.Run(ctx) }()
	go func() { errs <- server.Serve(listener) }()

	slog.Info("payments running", "grpc", grpcAddr, "commission_bps", commission, "group", group)

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

// openProcessor chooses who is asked to hold money.
//
// Stripe unless told otherwise, so a deploy that forgets to choose fails for
// want of a key, rather than starting on a processor that charges nobody and
// looking healthy while every ride is free.
func openProcessor() (service.Processor, error) {
	switch name := config.StringOr("PAYMENTS_PROCESSOR", "stripe"); name {
	case "fake":
		slog.Warn("PAYMENTS_PROCESSOR=fake: holds, captures and transfers are simulated, and no card is charged")
		return fake.New(), nil
	case "stripe":
		return nil, errors.New("payments: the Stripe processor is not wired in yet; set PAYMENTS_PROCESSOR=fake")
	default:
		return nil, &config.InvalidError{
			Key: "PAYMENTS_PROCESSOR", Value: name, Want: "processor",
			Err: errors.New("want stripe or fake"),
		}
	}
}

type metrics struct {
	applied *prometheus.CounterVec
	skipped *prometheus.CounterVec
	retries prometheus.Counter
}

func newMetrics(registry *obs.Registry) *metrics {
	m := &metrics{
		applied: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "surge_payments_facts_applied_total", Help: "Trip lifecycle facts acted on, by tag.",
		}, []string{"tag"}),
		skipped: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "surge_payments_facts_skipped_total",
			Help: "Trip facts deliberately moved past. `invalid_transition` is the state machine working; `unheld` is a trip booked without payments.",
		}, []string{"reason"}),
		retries: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "surge_payments_fact_retries_total",
			Help: "Attempts that failed transiently and were retried in place. Climbing means the processor or the database is unreachable.",
		}),
	}
	registry.MustRegister(m.applied, m.skipped, m.retries)
	return m
}
