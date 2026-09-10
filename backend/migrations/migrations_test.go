package migrations_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/ishakdeveloper/surge/migrations"
)

// The property the whole ledger-free arrangement rests on.
//
// Nothing records which migrations have run, so every one must be safe to apply
// to a database that already has it. A statement that is not would pass review,
// pass its first deploy, and fail on the next run of a script somebody trusted.
//
// This is the cheap half — a text check that every DDL statement is guarded.
// The expensive half is applying them twice against a real Postgres, which is
// what the trip service's integration test does.
func TestEveryStatementIsIdempotent(t *testing.T) {
	files, err := migrations.All()
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no migrations found; the embed is not working")
	}

	// Go's regexp is RE2 and has no lookahead, so this matches the DDL and
	// then checks the guard rather than trying to express "create without if
	// not exists" as one pattern.
	ddl := regexp.MustCompile(`(?im)^\s*create\s+(?:unique\s+)?(table|index|type|extension)\b[^;]*`)
	ordered := regexp.MustCompile(`^\d{4}_`)

	for _, file := range files {
		if !ordered.MatchString(file.Name) {
			t.Errorf("%s has no ordering prefix", file.Name)
		}

		for _, statement := range ddl.FindAllString(file.SQL, -1) {
			if !strings.Contains(strings.ToLower(statement), "if not exists") {
				head := strings.SplitN(strings.TrimSpace(statement), "\n", 2)[0]
				t.Errorf("%s: unguarded DDL, needs IF NOT EXISTS: %q", file.Name, head)
			}
		}
	}
}
