package tracing_test

import (
	"context"
	"testing"

	"github.com/ishakdeveloper/surge/pkg/tracing"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// The property the whole package exists for, and the one that silently breaks.
//
// A trace that does not survive the broker looks fine in every individual
// service and is useless the moment you have a question that spans two of them.
// Nothing about a missing header fails loudly — you just get four unrelated
// traces — so it is worth an explicit test.
func TestTraceContextSurvivesAKafkaRecord(t *testing.T) {
	otel.SetTracerProvider(sdktrace.NewTracerProvider())
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))

	producerCtx, producerSpan := tracing.Tracer("test").Start(context.Background(), "produce")
	defer producerSpan.End()

	produced := trace.SpanContextFromContext(producerCtx)
	if !produced.IsValid() {
		t.Fatal("setup: the producer span has no valid context")
	}

	// Inject exactly as Produce does, without needing a broker.
	record := &kgo.Record{Topic: "geo.events", Key: []byte("cell")}
	otel.GetTextMapPropagator().Inject(producerCtx, carrierFor(record))

	if len(record.Headers) == 0 {
		t.Fatal("no headers were written; the trace cannot cross the broker")
	}

	var hasTraceparent bool
	for _, header := range record.Headers {
		if header.Key == "traceparent" {
			hasTraceparent = true
		}
	}
	if !hasTraceparent {
		t.Fatalf("no traceparent header, got %+v", record.Headers)
	}

	// The consumer is a different process with a fresh context.
	_, consumerSpan := tracing.Consume(context.Background(), record, "consume")
	defer consumerSpan.End()

	consumed := consumerSpan.SpanContext()

	if consumed.TraceID() != produced.TraceID() {
		t.Errorf("trace id changed across the broker: %s became %s", produced.TraceID(), consumed.TraceID())
	}
	if consumed.SpanID() == produced.SpanID() {
		t.Error("the consumer reused the producer's span id rather than starting a child")
	}
}

// A record from a producer that had tracing switched off must still be
// consumable. Half a fleet running without an exporter is the normal state
// during a rollout.
func TestConsumeWithoutAnInboundContext(t *testing.T) {
	otel.SetTracerProvider(sdktrace.NewTracerProvider())

	record := &kgo.Record{Topic: "geo.events", Key: []byte("cell")}

	ctx, span := tracing.Consume(context.Background(), record, "consume")
	defer span.End()

	if ctx == nil {
		t.Fatal("Consume returned a nil context for an untraced record")
	}
	if !span.SpanContext().IsValid() {
		t.Error("an untraced record should still start a valid root span")
	}
}

// carrierFor mirrors the unexported carrier so the test can inject without
// going through a live Kafka client.
type headerCarrier struct{ record *kgo.Record }

func carrierFor(record *kgo.Record) propagation.TextMapCarrier {
	return headerCarrier{record: record}
}

func (c headerCarrier) Get(key string) string {
	for _, header := range c.record.Headers {
		if header.Key == key {
			return string(header.Value)
		}
	}
	return ""
}

func (c headerCarrier) Set(key, value string) {
	c.record.Headers = append(c.record.Headers, kgo.RecordHeader{Key: key, Value: []byte(value)})
}

func (c headerCarrier) Keys() []string {
	keys := make([]string, 0, len(c.record.Headers))
	for _, header := range c.record.Headers {
		keys = append(keys, header.Key)
	}
	return keys
}

// The footgun: a base URL is the documented convention, and using it verbatim
// makes the exporter POST to "/" and receive 404 forever without any symptom
// beyond an absence of traces.
func TestEndpointNormalisation(t *testing.T) {
	tests := map[string]string{
		"http://localhost:4318":            "http://localhost:4318/v1/traces",
		"http://localhost:4318/":           "http://localhost:4318/v1/traces",
		"http://localhost:4318/v1/traces":  "http://localhost:4318/v1/traces",
		"http://localhost:4318/v1/traces/": "http://localhost:4318/v1/traces",
	}

	for input, want := range tests {
		if got := tracing.TracesURL(input); got != want {
			t.Errorf("%q became %q, want %q", input, got, want)
		}
	}
}
