// Package pgtest gives a test a Postgres of its own.
//
// Its own schema, in the development database, with every migration applied to
// it — twice, because applying the whole set to a database that already has it
// is the contract the ledger-free migrator rests on, and this is where that
// contract is exercised against a real Postgres rather than a regex.
//
// A schema rather than a database because creating databases needs a role the
// services should not have, and a schema is dropped as completely.
package pgtest

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// URL is the database tests use: DATABASE_URL when set, the compose Postgres
// otherwise, so `go test ./...` works on a machine that has run `make up`
// without anybody exporting anything.
func URL() string {
	if url := os.Getenv("DATABASE_URL"); url != "" {
		return url
	}
	return "postgresql://surge:surge@localhost:55433/surge"
}

var sequence atomic.Int64

// Pool returns a pool whose connections see only a fresh schema holding the
// migrated tables. It skips the test when no Postgres is reachable.
func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	admin, err := pgx.Connect(ctx, URL())
	if err != nil {
		t.Skipf("no postgres reachable: %v", err)
	}
	defer func() { _ = admin.Close(context.Background()) }()

	schema := fmt.Sprintf("test_%s_%d",
		strconv.FormatInt(time.Now().UnixNano(), 36), sequence.Add(1))
	if _, err := admin.Exec(ctx, "create schema "+schema); err != nil {
		t.Fatalf("pgtest: create schema: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		conn, err := pgx.Connect(ctx, URL())
		if err != nil {
			return
		}
		defer func() { _ = conn.Close(ctx) }()
		_, _ = conn.Exec(ctx, "drop schema if exists "+schema+" cascade")
	})

	config, err := pgxpool.ParseConfig(URL())
	if err != nil {
		t.Fatalf("pgtest: parse url: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("pgtest: pool: %v", err)
	}
	t.Cleanup(pool.Close)

	files, err := migrations.All()
	if err != nil {
		t.Fatalf("pgtest: migrations: %v", err)
	}
	for pass := 1; pass <= 2; pass++ {
		for _, file := range files {
			if _, err := pool.Exec(ctx, file.SQL); err != nil {
				t.Fatalf("pgtest: apply %s (pass %d): %v", file.Name, pass, err)
			}
		}
	}

	return pool
}
