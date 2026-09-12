// Command fleet is who may drive, and what they may drive.
//
// Separate for isolation, the argument that separates payments and chat. It
// holds the identity provider's key and the documents bucket's credentials,
// and it calls three things outside the cluster — Stripe Identity, the Dutch
// vehicle register, and a model that reads certificates — none of which may
// stall a dispatch. Nothing calls it synchronously either: it publishes who
// may be offered work on a compacted topic, and whoever dispatches reads that.
//
// The payments service still holds the credentials that move money. This one's
// Stripe key should be a restricted key with Identity and nothing else, which
// is the difference between verifying a driver and being able to pay them.
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

	"github.com/ishakdeveloper/surge/services/fleet/internal/domain"
	"github.com/ishakdeveloper/surge/services/fleet/internal/infrastructure/fake"
	fleethandler "github.com/ishakdeveloper/surge/services/fleet/internal/infrastructure/grpc"
	"github.com/ishakdeveloper/surge/services/fleet/internal/infrastructure/identity"
	"github.com/ishakdeveloper/surge/services/fleet/internal/infrastructure/reader"
	"github.com/ishakdeveloper/surge/services/fleet/internal/infrastructure/register"
	"github.com/ishakdeveloper/surge/services/fleet/internal/infrastructure/repository"
	"github.com/ishakdeveloper/surge/services/fleet/internal/infrastructure/storage"
	"github.com/ishakdeveloper/surge/services/fleet/internal/service"
	"github.com/ishakdeveloper/surge/shared/authz"
	"github.com/ishakdeveloper/surge/shared/config"
	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/ishakdeveloper/surge/shared/obs"
	"github.com/ishakdeveloper/surge/shared/outbox"
	fleetpb "github.com/ishakdeveloper/surge/shared/proto/fleet"
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
		slog.Error("fleet exited", "error", err)
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

	metricsAddr := config.StringOr("FLEET_METRICS_ADDR", ":9110")
	// Listen address, not the address the gateway dials: the same split trip
	// and payments make.
	grpcAddr := config.StringOr("FLEET_GRPC_LISTEN", ":8115")

	sweepEvery, err := config.DurationOr("FLEET_SWEEP_INTERVAL", time.Hour)
	if err != nil {
		return err
	}

	// Required, with no in-memory fallback: a fleet that forgets who was
	// approved would put an unvetted driver on the road after a restart.
	databaseURL, err := config.String("DATABASE_URL")
	if err != nil {
		return err
	}

	store, err := openStore()
	if err != nil {
		return err
	}
	identityProvider, webhooks, err := openIdentity()
	if err != nil {
		return err
	}
	vehicleRegister, err := openRegister()
	if err != nil {
		return err
	}
	documentReader, err := openReader(store)
	if err != nil {
		return err
	}

	shutdownTracing, err := tracing.Init(ctx, "fleet", config.StringOr("OTEL_EXPORTER_OTLP_ENDPOINT", ""))
	if err != nil {
		return err
	}
	defer func() {
		flush, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = shutdownTracing(flush)
	}()

	registry := obs.NewRegistry("fleet")
	metrics := newMetrics(registry)

	if err := kafkax.EnsureTopics(ctx, cluster); err != nil {
		return err
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("fleet: connect: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("fleet: ping: %w", err)
	}

	producer, err := kafkax.NewProducer(cluster)
	if err != nil {
		return err
	}
	defer producer.Close()

	repo := repository.NewPostgres(pool)
	fleet, err := service.New(service.Options{
		Repository: repo,
		Identity:   identityProvider,
		Webhooks:   webhooks,
		Register:   vehicleRegister,
		Store:      store,
		Reader:     documentReader,
		Hooks: service.Hooks{
			OnStandingChanged: func(status domain.Status) {
				metrics.standings.WithLabelValues(string(status)).Inc()
			},
			OnExtraction: func(outcome string) { metrics.extractions.WithLabelValues(outcome).Inc() },
		},
	})
	if err != nil {
		return err
	}

	relay := outbox.NewRelay(pool, repository.OutboxTable, producer, outbox.Options{
		Hooks: outbox.PrometheusHooks(registry, "fleet"),
	})

	server := grpc.NewServer(
		opentelemetry.ServerOption(opentelemetry.Options{}),
		grpc.UnaryInterceptor(authz.UnaryServerInterceptor()),
	)
	fleetpb.RegisterFleetServiceServer(server, fleethandler.NewHandler(fleet))
	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(server, healthServer)
	healthServer.SetServingStatus("surge.fleet.v1.FleetService", healthpb.HealthCheckResponse_SERVING)
	reflection.Register(server)

	listener, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		return fmt.Errorf("fleet: listen %s: %w", grpcAddr, err)
	}

	errs := make(chan error, 3)
	go func() { errs <- registry.ServeMetrics(ctx, metricsAddr) }()
	go func() { errs <- relay.Run(ctx) }()
	go func() { errs <- server.Serve(listener) }()

	// The sweeper: papers that ran out overnight, and the standings that
	// follow from them. Every instance runs it, because expiring a document
	// twice is the same document and a standing is derived rather than
	// toggled.
	go func() {
		ticker := time.NewTicker(sweepEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				expired, err := fleet.Sweep(ctx)
				if err != nil && ctx.Err() == nil {
					slog.Warn("sweep did not finish; the next one retries", "error", err)
					continue
				}
				metrics.expired.Add(float64(expired))
			}
		}
	}()

	slog.Info("fleet running", "grpc", grpcAddr, "sweep", sweepEvery)

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

// openStore chooses where documents live. No default: a deploy that forgets to
// choose would keep a driver's papers on a laptop's heap.
func openStore() (service.Store, error) {
	switch name := config.StringOr("FLEET_DOCUMENT_STORE", ""); name {
	case "memory":
		slog.Warn("FLEET_DOCUMENT_STORE=memory: upload links go nowhere and nothing is kept")
		return storage.NewMemory(), nil
	case "r2":
		account, err := config.String("R2_ACCOUNT_ID")
		if err != nil {
			return nil, err
		}
		accessKey, err := config.String("R2_ACCESS_KEY_ID")
		if err != nil {
			return nil, err
		}
		secret, err := config.String("R2_SECRET_ACCESS_KEY")
		if err != nil {
			return nil, err
		}
		return storage.NewR2(storage.Options{
			Endpoint:        config.StringOr("R2_ENDPOINT", fmt.Sprintf("https://%s.r2.cloudflarestorage.com", account)),
			Bucket:          config.StringOr("FLEET_DOCUMENT_BUCKET", "surge-documents"),
			AccessKeyID:     accessKey,
			SecretAccessKey: secret,
		})
	default:
		return nil, &config.InvalidError{
			Key: "FLEET_DOCUMENT_STORE", Value: name, Want: "document store",
			Err: errors.New("want r2 or memory"),
		}
	}
}

// openIdentity chooses who verifies a driver. No default, for the reason
// payments has none: a deploy that forgets to choose would approve whoever
// asked.
func openIdentity() (service.Identity, service.Webhooks, error) {
	switch name := config.StringOr("FLEET_IDENTITY_PROVIDER", ""); name {
	case "fake":
		slog.Warn("FLEET_IDENTITY_PROVIDER=fake: nobody's licence is checked")
		return fake.NewIdentity(), nil, nil
	case "stripe":
		// A restricted key, scoped to Identity. Payments holds the key that
		// moves money, and this one cannot.
		key, err := config.String("STRIPE_IDENTITY_KEY")
		if err != nil {
			return nil, nil, err
		}
		secret, err := config.String("STRIPE_IDENTITY_WEBHOOK_SECRET")
		if err != nil {
			return nil, nil, err
		}
		provider := identity.New(key, secret)
		return provider, provider, nil
	default:
		return nil, nil, &config.InvalidError{
			Key: "FLEET_IDENTITY_PROVIDER", Value: name, Want: "identity provider",
			Err: errors.New("want stripe or fake"),
		}
	}
}

// openRegister chooses who answers about a plate. The RDW's open data needs no
// key and no account, so the real thing is the default and the fake is for
// working on a train.
func openRegister() (service.Register, error) {
	switch name := config.StringOr("FLEET_REGISTER", "rdw"); name {
	case "fake":
		slog.Warn("FLEET_REGISTER=fake: plates are answered from a table of five cars")
		return fake.NewRegister(time.Now()), nil
	case "rdw":
		return register.New(
			config.StringOr("RDW_ENDPOINT", register.Endpoint),
			config.StringOr("RDW_APP_TOKEN", ""),
		), nil
	default:
		return nil, &config.InvalidError{
			Key: "FLEET_REGISTER", Value: name, Want: "vehicle register",
			Err: errors.New("want rdw or fake"),
		}
	}
}

// openReader chooses what reads a certificate before a person does. Off is a
// reasonable answer: the reviewer reads the document either way, and this only
// saves them typing.
func openReader(store service.Store) (service.Reader, error) {
	switch name := config.StringOr("FLEET_READER", "off"); name {
	case "off":
		return nil, nil
	case "fake":
		return fake.NewReader(), nil
	case "claude":
		key, err := config.String("ANTHROPIC_API_KEY")
		if err != nil {
			return nil, err
		}
		return reader.New(reader.Options{
			Key:   key,
			Model: config.StringOr("FLEET_READER_MODEL", reader.DefaultModel),
			Store: store,
		})
	default:
		return nil, &config.InvalidError{
			Key: "FLEET_READER", Value: name, Want: "document reader",
			Err: errors.New("want claude, fake or off"),
		}
	}
}

type metrics struct {
	standings   *prometheus.CounterVec
	extractions *prometheus.CounterVec
	expired     prometheus.Counter
}

func newMetrics(registry *obs.Registry) *metrics {
	m := &metrics{
		standings: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "surge_fleet_standings_total",
			Help: "Drivers whose standing changed, by what it changed to.",
		}, []string{"status"}),
		extractions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "surge_fleet_extractions_total",
			Help: "Documents read by a machine, by outcome. `failed` is a reviewer typing it themselves, not an outage.",
		}, []string{"outcome"}),
		expired: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "surge_fleet_documents_expired_total",
			Help: "Approved documents the sweeper found had run out.",
		}),
	}
	registry.MustRegister(m.standings, m.extractions, m.expired)
	return m
}
