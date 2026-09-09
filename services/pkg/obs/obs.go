// Package obs is the metrics endpoint and tracing setup every service shares.
//
// Metrics are Prometheus, scraped from the host on a per-service port (9101+).
// Tracing is OTLP, and is installed only when OTEL_EXPORTER_OTLP_ENDPOINT is
// set — an exporter pointed at nothing retries on a schedule and fills the log,
// so off is the honest default. That mirrors what the Effect side already does
// in apps/auth/src/Telemetry.ts, deliberately: two runtimes, one rule.
package obs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"
)

// Registry is the per-process metric registry.
//
// Not the global default: promauto's default registry makes every metric
// package-global and untestable, and it panics on duplicate registration when
// two tests build the same service twice in one binary.
type Registry struct {
	*prometheus.Registry
	service string
}

func NewRegistry(service string) *Registry {
	registry := prometheus.NewRegistry()
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	return &Registry{Registry: registry, service: service}
}

// Service is the name this process reports as.
func (r *Registry) Service() string { return r.service }

// ServeMetrics exposes /metrics and a /health that answers as long as the
// process is scheduling goroutines. It returns once ctx is done, having shut
// the listener down.
func (r *Registry) ServeMetrics(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(r.Registry, promhttp.HandlerOpts{
		// A broken collector should show up as a scrape error rather than as a
		// silently missing series.
		ErrorHandling: promhttp.HTTPErrorOnError,
	}))
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("obs: metrics listener on %s: %w", addr, err)
	}
	return nil
}

// Tracing installs an OTLP exporter when endpoint is non-empty, and returns a
// shutdown function. With an empty endpoint it installs nothing and returns a
// no-op, so callers need no branch of their own.
func Tracing(ctx context.Context, service, endpoint string) (func(context.Context) error, error) {
	if endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
	if err != nil {
		return nil, fmt.Errorf("obs: otlp exporter: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		// Batched rather than synchronous: exporting a span per message would
		// put the collector on the critical path of every ping.
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(service),
		)),
	)

	otel.SetTracerProvider(provider)
	// W3C, so a browser span from packages/client stitches to the Go spans it
	// causes and a slow ride request is one waterfall rather than three.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))

	return provider.Shutdown, nil
}
