// Command chat is the conversations riders and drivers have: with each other
// about a trip, and with support.
//
// Separate for isolation, the argument that separates payments. It is the
// only process holding the push provider's credential and the only one
// calling it, and a slow Expo must never stall the trip state machine. Trip
// never calls it: trip publishes facts, this opens and closes conversations
// from them, and asks trip — as the caller — only whose trip something is.
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

	"github.com/ishakdeveloper/surge/services/chat/internal/infrastructure/events"
	"github.com/ishakdeveloper/surge/services/chat/internal/infrastructure/expo"
	"github.com/ishakdeveloper/surge/services/chat/internal/infrastructure/fake"
	chathandler "github.com/ishakdeveloper/surge/services/chat/internal/infrastructure/grpc"
	"github.com/ishakdeveloper/surge/services/chat/internal/infrastructure/repository"
	"github.com/ishakdeveloper/surge/services/chat/internal/infrastructure/trips"
	"github.com/ishakdeveloper/surge/services/chat/internal/service"
	"github.com/ishakdeveloper/surge/shared/authz"
	"github.com/ishakdeveloper/surge/shared/config"
	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/ishakdeveloper/surge/shared/obs"
	chatpb "github.com/ishakdeveloper/surge/shared/proto/chat"
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
		slog.Error("chat exited", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	brokers := config.Strings("KAFKA_BROKERS", []string{"localhost:19092"})
	metricsAddr := config.StringOr("CHAT_METRICS_ADDR", ":9108")
	// Listen address, not the address the gateway dials, as trip and payments
	// split them.
	grpcAddr := config.StringOr("CHAT_GRPC_LISTEN", ":8113")
	tripAddr := config.StringOr("TRIP_GRPC_ADDR", "localhost:8110")
	group := config.StringOr("CHAT_GROUP", "chat")

	closeGrace, err := config.DurationOr("CHAT_CLOSE_GRACE", service.DefaultCloseGrace)
	if err != nil {
		return err
	}
	pushDelay, err := config.DurationOr("CHAT_PUSH_DELAY", service.DefaultPushDelay)
	if err != nil {
		return err
	}
	rateLimit, err := config.IntOr("CHAT_RATE_LIMIT", service.DefaultRateLimit)
	if err != nil {
		return err
	}

	// Required, as for payments: a chat that forgets its messages on restart
	// is not one.
	databaseURL, err := config.String("DATABASE_URL")
	if err != nil {
		return err
	}

	pusher, err := openPusher()
	if err != nil {
		return err
	}

	shutdownTracing, err := tracing.Init(ctx, "chat", config.StringOr("OTEL_EXPORTER_OTLP_ENDPOINT", ""))
	if err != nil {
		return err
	}
	defer func() {
		flush, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = shutdownTracing(flush)
	}()

	registry := obs.NewRegistry("chat")
	metrics := newMetrics(registry)

	if err := kafkax.EnsureTopics(ctx, brokers); err != nil {
		return err
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("chat: connect: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("chat: ping: %w", err)
	}

	producer, err := kafkax.NewProducer(brokers)
	if err != nil {
		return err
	}
	defer producer.Close()

	tripClient, err := trips.Dial(tripAddr)
	if err != nil {
		return err
	}
	defer tripClient.Close()

	repo := repository.NewPostgres(pool)
	chat, err := service.New(service.Options{
		Repository: repo,
		Trips:      tripClient,
		Notifier:   events.NewNotifier(producer),
		CloseGrace: closeGrace,
		PushDelay:  pushDelay,
		RateLimit:  rateLimit,
	})
	if err != nil {
		return err
	}

	lifecycle, err := events.NewLifecycle(brokers, group, chat, events.Hooks{
		OnApplied: func(tag string) { metrics.applied.WithLabelValues(tag).Inc() },
		OnSkipped: func(reason string) { metrics.skipped.WithLabelValues(reason).Inc() },
		OnRetry:   func() { metrics.retries.Inc() },
	})
	if err != nil {
		return err
	}
	defer lifecycle.Close()

	worker := service.NewPushWorker(service.PushOptions{
		Store:  repo,
		Pusher: pusher,
		Hooks: service.PushHooks{
			OnSent:       func(n int) { metrics.pushed.WithLabelValues("sent").Add(float64(n)) },
			OnSuppressed: func(reason string, n int) { metrics.pushed.WithLabelValues(reason).Add(float64(n)) },
			OnFailed:     func() { metrics.pushFailures.Inc() },
		},
	})

	server := grpc.NewServer(
		opentelemetry.ServerOption(opentelemetry.Options{}),
		grpc.UnaryInterceptor(authz.UnaryServerInterceptor()),
	)
	chatpb.RegisterChatServiceServer(server, chathandler.NewHandler(chat))
	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(server, healthServer)
	healthServer.SetServingStatus("surge.chat.v1.ChatService", healthpb.HealthCheckResponse_SERVING)
	reflection.Register(server)

	listener, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		return fmt.Errorf("chat: listen %s: %w", grpcAddr, err)
	}

	errs := make(chan error, 4)
	go func() { errs <- registry.ServeMetrics(ctx, metricsAddr) }()
	go func() { errs <- lifecycle.Run(ctx) }()
	go func() { errs <- worker.Run(ctx, time.Second) }()
	go func() { errs <- server.Serve(listener) }()

	slog.Info("chat running", "grpc", grpcAddr, "trip", tripAddr, "group", group,
		"close_grace", closeGrace, "push_delay", pushDelay)

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

// openPusher chooses who delivers notifications. There is no default, so a
// deploy that forgets to choose fails at boot rather than running on the fake
// and looking healthy while nobody's phone ever buzzes.
func openPusher() (service.Pusher, error) {
	switch name := config.StringOr("CHAT_PUSH_PROVIDER", ""); name {
	case "fake":
		slog.Warn("CHAT_PUSH_PROVIDER=fake: notifications are logged, and no device is notified")
		return fake.NewPusher(), nil
	case "expo":
		return expo.New(config.StringOr("EXPO_PUSH_URL", expo.Endpoint), config.StringOr("EXPO_ACCESS_TOKEN", "")), nil
	default:
		return nil, &config.InvalidError{
			Key: "CHAT_PUSH_PROVIDER", Value: name, Want: "push provider",
			Err: errors.New("want expo or fake"),
		}
	}
}

type metrics struct {
	applied      *prometheus.CounterVec
	skipped      *prometheus.CounterVec
	retries      prometheus.Counter
	pushed       *prometheus.CounterVec
	pushFailures prometheus.Counter
}

func newMetrics(registry *obs.Registry) *metrics {
	m := &metrics{
		applied: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "surge_chat_facts_applied_total", Help: "Trip lifecycle facts acted on, by tag.",
		}, []string{"tag"}),
		skipped: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "surge_chat_facts_skipped_total",
			Help: "Trip facts moved past. `irrelevant` is a request or a driverless end, which has nobody to talk to.",
		}, []string{"reason"}),
		retries: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "surge_chat_fact_retries_total",
			Help: "Trip facts that failed and were retried in place. Climbing means the database is unreachable.",
		}),
		pushed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "surge_chat_pushes_total",
			Help: "Owed notifications, by outcome. `read` is one the recipient saw in time and never needed.",
		}, []string{"outcome"}),
		pushFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "surge_chat_push_failures_total",
			Help: "Batches the push provider refused, to be retried with backoff.",
		}),
	}
	registry.MustRegister(m.applied, m.skipped, m.retries, m.pushed, m.pushFailures)
	return m
}
