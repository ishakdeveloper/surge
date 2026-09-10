package outbox

import (
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// PrometheusHooks registers a relay's metrics for a service and returns hooks
// that feed them, so every outbox in the system reports the same three
// numbers under the same names.
func PrometheusHooks(registry interface{ MustRegister(...prometheus.Collector) }, service string) Hooks {
	published := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "surge_" + service + "_outbox_published_total",
		Help: "Facts relayed from the outbox to Kafka.",
	})
	lag := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "surge_" + service + "_outbox_lag_seconds",
		Help: "Age of the oldest fact in each relayed batch: how long a committed change waited to be heard.",
		// From one poll interval to a relay that has fallen badly behind.
		Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
	})
	failed := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "surge_" + service + "_outbox_errors_total",
		Help: "Relay flushes that failed and were retried.",
	})
	registry.MustRegister(published, lag, failed)

	return Hooks{
		OnPublished: func(count int, oldest time.Duration) {
			published.Add(float64(count))
			lag.Observe(oldest.Seconds())
		},
		OnError: func(err error) {
			failed.Inc()
			slog.Warn("outbox flush failed; it will be retried", "service", service, "error", err)
		},
	}
}
