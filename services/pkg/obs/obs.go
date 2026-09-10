// Package obs is the metrics endpoint every service shares.
//
// Metrics are Prometheus, scraped from the host on a per-service port (9101+).
// Tracing lives in pkg/tracing: two packages owning tracer setup would be two
// places to get the propagator wrong, and propagation is the part that matters.
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
