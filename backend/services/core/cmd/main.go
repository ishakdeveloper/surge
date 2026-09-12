// Command core holds everything boring about the people using Surge, as one
// service until it hurts — starting with profiles: the first name and the
// photo a rider and a driver see of each other.
//
// It is also the only process holding the object store's keys, as payments is
// the only one holding the processor's. A photo arrives here, is cropped and
// re-encoded — dropping whatever location a phone wrote into it — and only then
// stored; what leaves is a signed link.
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

	corehandler "github.com/ishakdeveloper/surge/services/core/internal/infrastructure/grpc"
	"github.com/ishakdeveloper/surge/services/core/internal/infrastructure/repository"
	"github.com/ishakdeveloper/surge/services/core/internal/infrastructure/storage"
	"github.com/ishakdeveloper/surge/services/core/internal/service"
	"github.com/ishakdeveloper/surge/shared/authz"
	"github.com/ishakdeveloper/surge/shared/config"
	"github.com/ishakdeveloper/surge/shared/obs"
	profilepb "github.com/ishakdeveloper/surge/shared/proto/profile"
	"github.com/ishakdeveloper/surge/shared/tracing"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/stats/opentelemetry"
)

// MaxMessage is what one call may carry: a 5 MB photo and the envelope
// around it. gRPC's own default of 4 MB would refuse the largest photos the
// API accepts.
const MaxMessage = 8 << 20

func main() {
	if err := run(); err != nil {
		slog.Error("core exited", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	metricsAddr := config.StringOr("CORE_METRICS_ADDR", ":9109")
	// Listen address, not the address the gateway dials, as the others split them.
	grpcAddr := config.StringOr("CORE_GRPC_LISTEN", ":8114")

	databaseURL, err := config.String("DATABASE_URL")
	if err != nil {
		return err
	}
	store, storeName, err := openStore()
	if err != nil {
		return err
	}

	shutdownTracing, err := tracing.Init(ctx, "core", config.StringOr("OTEL_EXPORTER_OTLP_ENDPOINT", ""))
	if err != nil {
		return err
	}
	defer func() {
		flush, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = shutdownTracing(flush)
	}()

	registry := obs.NewRegistry("core")

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("core: connect: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("core: ping: %w", err)
	}

	profiles := service.New(repository.NewPostgres(pool), store, time.Now)

	server := grpc.NewServer(
		opentelemetry.ServerOption(opentelemetry.Options{}),
		grpc.UnaryInterceptor(authz.UnaryServerInterceptor()),
		grpc.MaxRecvMsgSize(MaxMessage),
	)
	profilepb.RegisterProfileServiceServer(server, corehandler.NewHandler(profiles))
	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(server, healthServer)
	healthServer.SetServingStatus("surge.profile.v1.ProfileService", healthpb.HealthCheckResponse_SERVING)
	reflection.Register(server)

	listener, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		return fmt.Errorf("core: listen %s: %w", grpcAddr, err)
	}

	errs := make(chan error, 2)
	go func() { errs <- registry.ServeMetrics(ctx, metricsAddr) }()
	go func() { errs <- server.Serve(listener) }()

	slog.Info("core running", "grpc", grpcAddr, "photos", storeName)

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

// openStore chooses where photos live. There is no default, as payments has
// none for its processor: a deploy that forgets to choose fails at boot rather
// than keeping everyone's photo in memory until the next restart.
func openStore() (service.Store, string, error) {
	switch name := config.StringOr("AVATAR_STORE", ""); name {
	case "r2":
		account, err := config.String("R2_ACCOUNT_ID")
		if err != nil {
			return nil, "", err
		}
		keyID, err := config.String("R2_ACCESS_KEY_ID")
		if err != nil {
			return nil, "", err
		}
		secret, err := config.String("R2_SECRET_ACCESS_KEY")
		if err != nil {
			return nil, "", err
		}
		store, err := storage.NewR2(storage.R2Options{
			Endpoint:        config.StringOr("R2_ENDPOINT", "https://"+account+".r2.cloudflarestorage.com"),
			Bucket:          config.StringOr("R2_BUCKET", "surge"),
			AccessKeyID:     keyID,
			SecretAccessKey: secret,
		})
		return store, "r2", err
	case "memory":
		slog.Warn("AVATAR_STORE=memory: photos live in this process and are gone when it stops")
		return storage.NewMemory(), "memory", nil
	case "":
		return nil, "", errors.New("core: AVATAR_STORE is unset; choose r2, or memory for a laptop without R2 keys")
	default:
		return nil, "", fmt.Errorf("core: AVATAR_STORE=%q is neither r2 nor memory", name)
	}
}
