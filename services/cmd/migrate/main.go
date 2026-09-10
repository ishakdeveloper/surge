// Command migrate applies the Go services' SQL migrations.
//
// Run as a step, never at boot: two instances starting together would both
// migrate. Every file is idempotent and there is no ledger, so applying the
// whole set to any database converges it on the committed schema — the same
// contract packages/database keeps on the TypeScript side.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/ishakdeveloper/surge/migrations"
	"github.com/ishakdeveloper/surge/pkg/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(); err != nil {
		slog.Error("migrate failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	url, err := config.String("DATABASE_URL")
	if err != nil {
		return err
	}

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return fmt.Errorf("migrate: connect: %w", err)
	}
	defer pool.Close()

	files, err := migrations.All()
	if err != nil {
		return err
	}

	for _, file := range files {
		if _, err := pool.Exec(ctx, file.SQL); err != nil {
			return fmt.Errorf("migrate: apply %s: %w", file.Name, err)
		}
		slog.Info("applied", "migration", file.Name)
	}

	slog.Info("migrations complete", "count", len(files))
	return nil
}
