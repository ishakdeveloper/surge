package outbox_test

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/ishakdeveloper/surge/shared/outbox"
	"github.com/ishakdeveloper/surge/shared/pgtest"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

var table = outbox.NewTable("test_outbox")

func brokers() []string {
	if raw := os.Getenv("KAFKA_BROKERS"); raw != "" {
		return strings.Split(raw, ",")
	}
	return []string{"localhost:19092"}
}

type rig struct {
	pool     *pgxpool.Pool
	producer *kgo.Client
	topic    string
}

// newRig needs both halves of the outbox — a Postgres to commit to and a
// broker to relay to — and skips unless both are there.
func newRig(t *testing.T) *rig {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool := pgtest.Pool(t)
	if _, err := pool.Exec(ctx, `
		create table test_outbox (
		  id bigint generated always as identity primary key,
		  topic text not null, key text not null, value bytea not null,
		  headers jsonb not null default '{}', created_at timestamptz not null default now()
		)`); err != nil {
		t.Fatalf("create outbox table: %v", err)
	}

	client, err := kgo.NewClient(kgo.SeedBrokers(brokers()...))
	if err != nil {
		t.Fatalf("kafka client: %v", err)
	}
	t.Cleanup(client.Close)
	admin := kadm.NewClient(client)
	if _, err := admin.ListBrokers(ctx); err != nil {
		t.Skipf("no broker at %v: %v", brokers(), err)
	}

	topic := "outbox-test-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	if _, err := admin.CreateTopics(ctx, 1, 1, nil, topic); err != nil {
		t.Fatalf("create topic: %v", err)
	}
	t.Cleanup(func() { _, _ = admin.DeleteTopics(context.Background(), topic) })

	producer, err := kafkax.NewProducer(brokers())
	if err != nil {
		t.Fatalf("producer: %v", err)
	}
	t.Cleanup(producer.Close)

	return &rig{pool: pool, producer: producer, topic: topic}
}

func (r *rig) write(t *testing.T, commit bool, values ...int) {
	t.Helper()
	ctx := context.Background()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	for _, value := range values {
		if err := outbox.Write(ctx, tx, table, outbox.Message{
			Topic: r.topic, Key: "trip-1", Value: map[string]int{"n": value},
		}); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if commit {
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit: %v", err)
		}
		return
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}
}

func (r *rig) pending(t *testing.T) int {
	t.Helper()
	var count int
	if err := r.pool.QueryRow(context.Background(), `select count(*) from test_outbox`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	return count
}

// consume reads `want` records from the start of the topic.
func (r *rig) consume(t *testing.T, want int) []*kgo.Record {
	t.Helper()
	consumer, err := kgo.NewClient(
		kgo.SeedBrokers(brokers()...),
		kgo.ConsumeTopics(r.topic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	if err != nil {
		t.Fatalf("consumer: %v", err)
	}
	defer consumer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var records []*kgo.Record
	for len(records) < want {
		fetches := consumer.PollFetches(ctx)
		if ctx.Err() != nil {
			t.Fatalf("read %d of %d records before timing out", len(records), want)
		}
		records = append(records, fetches.Records()...)
	}
	return records
}

func TestCommittedRowsArePublishedInOrderAndRemoved(t *testing.T) {
	rig := newRig(t)
	rig.write(t, true, 1, 2, 3)

	relay := outbox.NewRelay(rig.pool, table, rig.producer, outbox.Options{})
	published, err := relay.Flush(context.Background())
	if err != nil {
		t.Fatalf("flush: %v", err)
	}
	if published != 3 {
		t.Fatalf("published %d, want 3", published)
	}
	if left := rig.pending(t); left != 0 {
		t.Errorf("%d rows left in the outbox after publishing", left)
	}

	for i, record := range rig.consume(t, 3) {
		var body map[string]int
		if err := json.Unmarshal(record.Value, &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body["n"] != i+1 || string(record.Key) != "trip-1" {
			t.Errorf("record %d: key %q body %v, want n=%d in commit order", i, record.Key, body, i+1)
		}
	}
}

// The whole point: a change that rolled back is a fact nobody hears.
func TestARolledBackWriteIsNeverPublished(t *testing.T) {
	rig := newRig(t)
	rig.write(t, false, 1)

	relay := outbox.NewRelay(rig.pool, table, rig.producer, outbox.Options{})
	if published, err := relay.Flush(context.Background()); err != nil || published != 0 {
		t.Fatalf("flushed %d (%v) after a rollback, want 0", published, err)
	}
}

// Only one relay publishes at a time, or two instances would each take half a
// batch and publish one trip's facts in whichever order their brokers
// answered.
func TestOneRelayAtATime(t *testing.T) {
	rig := newRig(t)
	rig.write(t, true, 1)
	ctx := context.Background()

	holder, err := rig.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := holder.Exec(ctx, `select pg_advisory_xact_lock(hashtext($1))`, table.String()); err != nil {
		t.Fatalf("take the lock: %v", err)
	}

	relay := outbox.NewRelay(rig.pool, table, rig.producer, outbox.Options{})
	if published, err := relay.Flush(ctx); err != nil || published != 0 {
		t.Fatalf("flushed %d (%v) while another relay held the table", published, err)
	}

	if err := holder.Rollback(ctx); err != nil && err != pgx.ErrTxClosed {
		t.Fatalf("release: %v", err)
	}
	if published, err := relay.Flush(ctx); err != nil || published != 1 {
		t.Fatalf("flushed %d (%v) once the lock was free, want 1", published, err)
	}
}

func TestTableNamesAreIdentifiers(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("a table name with SQL in it was accepted")
		}
	}()
	outbox.NewTable("trip_outbox; drop table trip")
}
