// Command create-service scaffolds a new microservice.
//
// The layout below is not decoration. `domain` holds the rules and imports no
// transport, so it can be tested by calling functions; `service` holds the
// logic and depends on domain interfaces; `infrastructure` is the only place
// that knows about Kafka, gRPC, Postgres or HTTP. `internal` means Go itself
// enforces that nothing outside this service reaches into it.
//
//	go run ./tools/create-service -name pricing
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

func main() {
	name := flag.String("name", "", "service name, lowercase, e.g. pricing")
	root := flag.String("root", "services", "where services live")
	flag.Parse()

	if err := run(*name, *root); err != nil {
		fmt.Fprintln(os.Stderr, "create-service:", err)
		os.Exit(1)
	}
}

func run(name, root string) error {
	if name == "" {
		return fmt.Errorf("-name is required")
	}
	if name != strings.ToLower(name) || strings.ContainsAny(name, " _/") {
		// The name becomes a directory, a package path and a binary. Rejecting
		// the awkward cases here beats discovering them in an import path.
		return fmt.Errorf("name must be lowercase with no spaces, underscores or slashes")
	}

	dir := filepath.Join(root, name)
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("%s already exists", dir)
	}

	data := struct {
		Name  string
		Title string
	}{Name: name, Title: strings.ToUpper(name[:1]) + name[1:]}

	for path, body := range files {
		name, err := render(path, data)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}

		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}

		rendered, err := render(body, data)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if err := os.WriteFile(full, []byte(rendered), 0o644); err != nil {
			return err
		}
	}

	fmt.Printf("created %s\n\n", dir)
	fmt.Printf("  1. add a build line to the Makefile:  $(GO) build -o bin/%s ./services/%s/cmd\n", name, name)
	fmt.Printf("  2. add a Dockerfile:                  deploy/docker/%s.Dockerfile\n", name)
	fmt.Printf("  3. give it a metrics port in deploy/prometheus/prometheus.yml\n")
	fmt.Printf("  4. go build ./services/%s/...\n", name)
	return nil
}

func render(body string, data any) (string, error) {
	parsed, err := template.New("file").Funcs(template.FuncMap{
		"toUpper": strings.ToUpper,
	}).Parse(body)
	if err != nil {
		return "", err
	}

	var out strings.Builder
	if err := parsed.Execute(&out, data); err != nil {
		return "", err
	}
	return out.String(), nil
}

var files = map[string]string{
	"cmd/main.go": `// Command {{.Name}} does one thing.
//
// Say what, and why it is separate from everything else. A service that cannot
// answer "what does this scale on that nothing else does" probably belongs
// inside one that already exists.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ishakdeveloper/surge/shared/config"
	"github.com/ishakdeveloper/surge/shared/obs"
	"github.com/ishakdeveloper/surge/shared/tracing"
)

func main() {
	if err := run(); err != nil {
		slog.Error("{{.Name}} exited", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	metricsAddr := config.StringOr("{{.Name | toUpper}}_METRICS_ADDR", ":9110")

	shutdownTracing, err := tracing.Init(ctx, "{{.Name}}",
		config.StringOr("OTEL_EXPORTER_OTLP_ENDPOINT", ""))
	if err != nil {
		return err
	}
	defer func() {
		flush, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = shutdownTracing(flush)
	}()

	registry := obs.NewRegistry("{{.Name}}")

	errs := make(chan error, 1)
	go func() { errs <- registry.ServeMetrics(ctx, metricsAddr) }()

	select {
	case <-ctx.Done():
		return nil
	case err := <-errs:
		return err
	}
}
`,

	"internal/domain/{{.Name}}.go": `// Package domain is {{.Name}}'s rules, expressed without reference to how they
// are stored or transported.
//
// Nothing here imports gRPC, Kafka or Postgres. That is what lets the part
// worth being sure about be tested by calling functions rather than by standing
// up a broker.
package domain
`,

	"internal/service/service.go": `// Package service is {{.Name}}'s logic.
//
// It depends on domain interfaces and nothing else, so its tests run against
// in-memory implementations and finish in milliseconds.
package service
`,

	"internal/infrastructure/events/events.go": `// Package events is {{.Name}}'s Kafka edge.
//
// Produce with tracing.Produce and consume with tracing.Consume, so this
// service's work joins the trace of whatever caused it rather than starting a
// new one.
package events
`,

	"README.md": `# {{.Name}}

One paragraph: what this service owns, and what it scales on that nothing else
does. If that paragraph is hard to write, this probably belongs inside a service
that already exists.

## Layout

    cmd/                      entrypoint and wiring
    internal/domain/          rules, no transport
    internal/service/         logic, depends on domain interfaces
    internal/infrastructure/  kafka, grpc, postgres, http

## Ports

| | |
|---|---|
| metrics | :91xx |

## Configuration

Read through ` + "`shared/config`" + `, which fails loudly: an unset variable may
take a default, a set-but-unparseable one is always an error.
`,
}
