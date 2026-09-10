package sim

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/ishakdeveloper/surge/shared/wire"
	"github.com/twmb/franz-go/pkg/kgo"
)

// Transport is how a simulated driver reaches the system.
//
// An interface with two implementations, and the reason is sequencing. Phase 1
// has no gateway yet, so drivers produce straight to Kafka and the load rig can
// exist before the product does. Phase 3 swaps in a WebSocket transport and the
// same simulator suddenly opens 10,000 real connections through the real
// gateway, exercising the connection registry and slow-consumer eviction.
//
// Designing for that swap now costs one interface. Discovering the need for it
// later costs rewriting the simulator.
type Transport interface {
	Ping(ctx context.Context, ping wire.DriverPing) error
	// Flush blocks until buffered messages have been delivered.
	Flush(ctx context.Context) error
	Close()
}

// KafkaTransport produces pings directly to loc.ping.
type KafkaTransport struct {
	client  *kgo.Client
	onError func(error)
}

func NewKafkaTransport(brokers []string, onError func(error)) (*KafkaTransport, error) {
	client, err := kafkax.NewProducer(brokers)
	if err != nil {
		return nil, fmt.Errorf("sim: kafka transport: %w", err)
	}
	return &KafkaTransport{client: client, onError: onError}, nil
}

func (t *KafkaTransport) Ping(ctx context.Context, ping wire.DriverPing) error {
	payload, err := json.Marshal(ping)
	if err != nil {
		return fmt.Errorf("sim: encode ping: %w", err)
	}

	// Keyed by driver, so one driver's pings land in one partition and stay
	// ordered relative to each other. Ordering across drivers is meaningless
	// and buying it would serialise the whole topic.
	record := &kgo.Record{
		Topic: kafkax.TopicLocPing,
		Key:   []byte(ping.DriverID),
		Value: payload,
	}

	// Asynchronous, and deliberately so. A synchronous produce per ping would
	// make every driver goroutine wait on a network round trip, which caps the
	// simulator's rate at (goroutines / RTT) rather than at whatever the broker
	// can absorb — measuring the simulator instead of the system.
	t.client.Produce(ctx, record, func(_ *kgo.Record, err error) {
		if err != nil && t.onError != nil {
			t.onError(err)
		}
	})

	return nil
}

func (t *KafkaTransport) Flush(ctx context.Context) error { return t.client.Flush(ctx) }
func (t *KafkaTransport) Close()                          { t.client.Close() }

// DiscardTransport counts pings and drops them.
//
// The control: it measures what the simulator itself costs, so a throughput
// ceiling can be attributed to the load generator or to the system under test
// rather than guessed at. A benchmark without this number is not evidence.
type DiscardTransport struct{ onPing func() }

func NewDiscardTransport(onPing func()) *DiscardTransport { return &DiscardTransport{onPing: onPing} }

func (t *DiscardTransport) Ping(context.Context, wire.DriverPing) error {
	if t.onPing != nil {
		t.onPing()
	}
	return nil
}

func (t *DiscardTransport) Flush(context.Context) error { return nil }
func (t *DiscardTransport) Close()                      {}
