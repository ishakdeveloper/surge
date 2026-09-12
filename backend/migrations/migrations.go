// Package migrations embeds the Go services' SQL.
//
// A package rather than a directory the migrator reads at runtime, because an
// embed directive cannot reach up out of its own directory, and because a
// binary that carries its own schema cannot be deployed without it.
package migrations

import (
	"embed"
	"fmt"
	"sort"
)

//go:embed *.sql
var files embed.FS

// File is one migration, in the order it must be applied.
type File struct {
	Name string
	SQL  string
}

// All returns every migration, ordered by filename. Applying them out of order
// would fail on a foreign key, so the numeric prefix is load-bearing rather
// than decorative.
func All() ([]File, error) {
	entries, err := files.ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf("migrations: read: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	out := make([]File, 0, len(names))
	for _, name := range names {
		body, err := files.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("migrations: read %s: %w", name, err)
		}
		out = append(out, File{Name: name, SQL: string(body)})
	}
	return out, nil
}
