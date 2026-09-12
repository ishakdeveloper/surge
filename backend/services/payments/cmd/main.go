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
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ishakdeveloper/surge/services/payments/internal/infrastructure/events"
	"github.com/ishakdeveloper/surge/services/payments/internal/infrastructure/fake"
	paymentshandler "github.com/ishakdeveloper/surge/services/payments/internal/infrastructure/grpc"
	"github.com/ishakdeveloper/surge/services/payments/internal/infrastructure/repository"
	surgestripe "github.com/ishakdeveloper/surge/services/payments/internal/infrastructure/stripe"
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
	"google.golang.org/grpc/keepalive"
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

	cluster, err := kafkax.ClusterFromEnv()
	if err != nil {
		return err
	}
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
	sweepEvery, err := config.DurationOr("PAYMENTS_SWEEP_INTERVAL", time.Minute)
	if err != nil {
		return err
	}
	actionTimeout, err := config.DurationOr("PAYMENTS_ACTION_TIMEOUT", service.DefaultSweepPolicy.Action)
	if err != nil {
		return err
	}
	holdMaxAge, err := config.DurationOr("PAYMENTS_HOLD_MAX_AGE", service.DefaultSweepPolicy.Hold)
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

	processor, stripeProcessor, err := openProcessor()
	if err != nil {
		return err
	}
	if stripeProcessor != nil && plainHTTPOffLocalhost(webURL) {
		// Onboarding links send the driver's browser back here. Stripe takes
		// http://localhost for that in test mode — checked against the
		// sandbox — but a real host has to be HTTPS. Said at boot rather than
		// in a driver's failed click.
		slog.Warn("PAYMENTS_WEB_URL is plain http on a real host; Stripe will refuse driver onboarding links",
			"web_url", webURL)
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

	if err := kafkax.EnsureTopics(ctx, cluster); err != nil {
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

	producer, err := kafkax.NewProducer(cluster)
	if err != nil {
		return err
	}
	defer producer.Close()

	repo := repository.NewPostgres(pool)
	payments, err := service.New(service.Options{
		Repository:    repo,
		Processor:     processor,
		CommissionBps: commission,
		WebURL:        webURL,
		Sweep:         service.SweepPolicy{Action: actionTimeout, Hold: holdMaxAge},
		// Every change is pushed to the people it concerns, which is what
		// keeps the rider's and driver's screens live without polling.
		Notifier: events.NewNotifier(producer),
	})
	if err != nil {
		return err
	}

	relay := outbox.NewRelay(pool, repository.OutboxTable, producer, outbox.Options{
		Hooks: outbox.PrometheusHooks(registry, "payments"),
	})

	consumer, err := events.NewConsumer(cluster, group, payments, events.Hooks{
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
		// Recycled, so the gateway re-resolves the headless Service and finds
		// pods that started after it connected.
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionAge:      5 * time.Minute,
			MaxConnectionAgeGrace: 30 * time.Second,
		}),
		// The caller the gateway verified, back out of metadata, so a handler
		// reads who is asking from context and never from the request.
		grpc.UnaryInterceptor(authz.UnaryServerInterceptor()),
	)
	var webhooks paymentshandler.WebhookReceiver
	if stripeProcessor != nil {
		secret, err := config.String("STRIPE_WEBHOOK_SECRET")
		if err != nil {
			return err
		}
		// A deployed event destination signs with its own secret; `stripe
		// listen` signs both kinds with one, so this defaults to it.
		thinSecret := config.StringOr("STRIPE_THIN_WEBHOOK_SECRET", secret)
		webhooks = surgestripe.NewWebhooks(stripeProcessor, payments, repo, secret, thinSecret)
	}
	paymentspb.RegisterPaymentsServiceServer(server, paymentshandler.NewHandler(payments, webhooks))
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

	// The sweeper: stuck holds finished, expired or released, and drivers
	// paid once they can be. Every instance runs it; see Service.Sweep for
	// why that is safe.
	go func() {
		ticker := time.NewTicker(sweepEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				result, err := payments.Sweep(ctx)
				metrics.swept.WithLabelValues("resumed").Add(float64(result.Resumed))
				metrics.swept.WithLabelValues("expired").Add(float64(result.Expired))
				metrics.swept.WithLabelValues("released").Add(float64(result.Released))
				metrics.swept.WithLabelValues("drivers_paid").Add(float64(result.DriversPaid))
				if err != nil && ctx.Err() == nil {
					slog.Warn("sweep did not finish everything; the next one retries", "error", err)
				}
			}
		}
	}()

	slog.Info("payments running", "grpc", grpcAddr, "commission_bps", commission, "group", group)

	var failed error
	select {
	case <-ctx.Done():
	case failed = <-errs:
	}

	// NOT_SERVING first, so the readiness probe takes this pod out of rotation
	// while GracefulStop lets the calls already in flight finish.
	healthServer.Shutdown()
	server.GracefulStop()
	if failed == nil || errors.Is(failed, grpc.ErrServerStopped) {
		return nil
	}
	return failed
}

// openProcessor chooses who is asked to hold money.
//
// Stripe unless told otherwise, so a deploy that forgets to choose fails for
// want of a key, rather than starting on a processor that charges nobody and
// looking healthy while every ride is free.
//
// The Stripe processor is also returned on its own, because only it has
// webhooks to receive.
func openProcessor() (service.Processor, *surgestripe.Processor, error) {
	switch name := config.StringOr("PAYMENTS_PROCESSOR", "stripe"); name {
	case "fake":
		slog.Warn("PAYMENTS_PROCESSOR=fake: holds, captures and transfers are simulated, and no card is charged")
		return fake.New(), nil, nil
	case "stripe":
		key, err := config.String("STRIPE_SECRET_KEY")
		if err != nil {
			return nil, nil, err
		}
		if strings.HasPrefix(key, "sk_live_") {
			// Not refused — a platform may run on a secret key — but said,
			// because a restricted key limits what a leaked one can do.
			slog.Warn("STRIPE_SECRET_KEY is a live secret key; a restricted key (rk_live_) is safer")
		}
		stripeProcessor := surgestripe.New(key, surgestripe.Options{
			PaymentMethodConfiguration: config.StringOr("PAYMENTS_PAYMENT_METHOD_CONFIG", ""),
		})
		return stripeProcessor, stripeProcessor, nil
	default:
		return nil, nil, &config.InvalidError{
			Key: "PAYMENTS_PROCESSOR", Value: name, Want: "processor",
			Err: errors.New("want stripe or fake"),
		}
	}
}

// plainHTTPOffLocalhost reports a web URL Stripe will not send a driver back
// to: http on anything but the loopback host.
func plainHTTPOffLocalhost(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "http" {
		return false
	}
	switch parsed.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return false
	default:
		return true
	}
}

type metrics struct {
	applied *prometheus.CounterVec
	skipped *prometheus.CounterVec
	retries prometheus.Counter
	swept   *prometheus.CounterVec
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
	m.swept = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "surge_payments_swept_total",
		Help: "Stuck payments the sweeper acted on, by what it did. `released` climbing means trips are not being completed.",
	}, []string{"kind"})
	registry.MustRegister(m.applied, m.skipped, m.retries, m.swept)
	return m
}
