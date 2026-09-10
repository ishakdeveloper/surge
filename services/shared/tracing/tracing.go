// Package tracing is distributed tracing across process and transport
// boundaries.
//
// The problem it solves is specific. A ride touches four processes and two
// transports: a request arrives over HTTP at the gateway, becomes a Kafka
// record consumed by a matcher, produces another Kafka record consumed by the
// trip service, and comes back as a push. Each of those hops starts its own
// span, and without propagation you get four unrelated traces and no way to ask
// "what happened to trip-1234".
//
// Context propagation is what turns them into one trace. The W3C `traceparent`
// header travels in the Kafka record headers, exactly as it travels in HTTP
// headers, so the span a matcher creates is a child of the span the gateway
// created — across a broker, a process boundary, and an hour of queueing.
package tracing

import (
	"context"
	"fmt"
	"strings"

	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"
	"go.opentelemetry.io/otel/trace"
)

// Init installs the tracer and the propagator, returning a shutdown function.
//
// With an empty endpoint it installs nothing and returns a no-op, so callers
// need no branch of their own — an exporter pointed at nothing retries on a
// schedule and fills the log, and off is the honest default for a fresh clone.
func Init(ctx context.Context, service, endpoint string) (func(context.Context) error, error) {
	// The propagator is installed either way. It costs nothing without an
	// exporter, and installing it unconditionally means a service started
	// without tracing still forwards the context of one that has it — so
	// turning tracing on for one process does not produce a broken chain.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))

	if endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(TracesURL(endpoint)))
	if err != nil {
		return nil, fmt.Errorf("tracing: otlp exporter: %w", err)
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

	return provider.Shutdown, nil
}

// TracesURL normalises what people actually put in OTEL_EXPORTER_OTLP_ENDPOINT.
//
// The convention is a base URL — `http://localhost:4318` — and every collector
// serves traces at `/v1/traces` under it. But WithEndpointURL uses the URL
// verbatim, so a base URL posts to `/` and the collector answers 404 forever,
// on a retry schedule, filling the log with a failure that never surfaces as
// missing traces because the traces were never there to miss.
//
// Accepting both spellings costs three lines and removes a footgun that takes
// an afternoon to find. The Effect side does the same in Telemetry.ts.
func TracesURL(endpoint string) string {
	trimmed := strings.TrimRight(endpoint, "/")
	if strings.HasSuffix(trimmed, "/v1/traces") {
		return trimmed
	}
	return trimmed + "/v1/traces"
}

// Tracer returns a named tracer.
func Tracer(name string) trace.Tracer { return otel.GetTracerProvider().Tracer(name) }

// recordCarrier adapts Kafka record headers to OpenTelemetry's carrier
// interface, which is what lets the same `traceparent` that travels in an HTTP
// header travel through a broker instead.
type recordCarrier struct{ record *kgo.Record }

func (c recordCarrier) Get(key string) string {
	for _, header := range c.record.Headers {
		if header.Key == key {
			return string(header.Value)
		}
	}
	return ""
}

func (c recordCarrier) Set(key, value string) {
	for i, header := range c.record.Headers {
		if header.Key == key {
			c.record.Headers[i].Value = []byte(value)
			return
		}
	}
	c.record.Headers = append(c.record.Headers, kgo.RecordHeader{Key: key, Value: []byte(value)})
}

func (c recordCarrier) Keys() []string {
	keys := make([]string, 0, len(c.record.Headers))
	for _, header := range c.record.Headers {
		keys = append(keys, header.Key)
	}
	return keys
}

// Produce starts a producer span, injects the trace context into the record's
// headers, and hands the record to the client.
//
// The span ends when the broker acknowledges, so its duration is the produce
// latency rather than the time to enqueue — which is the number worth having
// when a partition is slow.
func Produce(ctx context.Context, client *kgo.Client, record *kgo.Record, done func(*kgo.Record, error)) {
	ctx, span := Tracer("kafka").Start(ctx,
		fmt.Sprintf("produce %s", record.Topic),
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			semconv.MessagingSystemKafka,
			semconv.MessagingDestinationName(record.Topic),
			attribute.String("messaging.kafka.message_key", string(record.Key)),
		),
	)

	otel.GetTextMapPropagator().Inject(ctx, recordCarrier{record: record})

	client.Produce(ctx, record, func(r *kgo.Record, err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetAttributes(attribute.Int64("messaging.kafka.partition", int64(r.Partition)))
		}
		span.End()

		if done != nil {
			done(r, err)
		}
	})
}

// Consume extracts the producer's context from a record and starts a consumer
// span as its child.
//
// The caller must end the span. Returned rather than deferred internally
// because the interesting work — decoding, handling, producing more records —
// happens in the caller, and a span that ended before it would time nothing.
func Consume(ctx context.Context, record *kgo.Record, operation string) (context.Context, trace.Span) {
	ctx = otel.GetTextMapPropagator().Extract(ctx, recordCarrier{record: record})

	return Tracer("kafka").Start(ctx, operation,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			semconv.MessagingSystemKafka,
			semconv.MessagingDestinationName(record.Topic),
			attribute.Int64("messaging.kafka.partition", int64(record.Partition)),
			attribute.Int64("messaging.kafka.offset", record.Offset),
			attribute.String("messaging.kafka.message_key", string(record.Key)),
		),
	)
}

// Fail marks a span as failed. A small helper because the two-line form gets
// written wrong — recording the error without setting the status leaves the
// span green in every UI.
func Fail(span trace.Span, err error) {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}
