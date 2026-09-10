// Package outbox makes "change the database and tell Kafka" one decision
// instead of two.
//
// Without it, a service writes a row and then produces a record, and a crash
// between the two is a fact the rest of the system never hears: a trip that
// completed and was never charged, because the record that said so died with
// the process. The trip service did exactly that until money depended on it.
//
// With it, the record is written into a table in the same transaction as the
// change it describes, so the two commit or roll back together. A relay then
// moves committed rows to Kafka. Delivery is at least once — a relay that
// crashes after the broker acknowledged and before it deleted the rows sends
// them again — so every consumer of an outbox-fed topic must be idempotent,
// which the ones here are by construction: state machines refuse a transition
// they have already made, and Stripe calls carry idempotency keys.
package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/ishakdeveloper/surge/shared/tracing"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// Table is an outbox table's name.
//
// A type rather than a string because the name is spliced into SQL — a table
// cannot be a bind parameter — so it is validated once, where it is declared,
// rather than trusted at every call.
type Table struct{ name string }

var identifier = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// NewTable declares an outbox table. It panics on a name that is not a plain
// identifier: that is a programming error in a constant, not a runtime
// condition anybody could handle.
func NewTable(name string) Table {
	if !identifier.MatchString(name) {
		panic(fmt.Sprintf("outbox: %q is not a valid table name", name))
	}
	return Table{name: name}
}

func (t Table) String() string { return t.name }

// Message is one record to publish once the transaction commits.
type Message struct {
	Topic string
	Key   string
	// Value is encoded as JSON at write time, so the bytes the relay publishes
	// are exactly the bytes the writer produced.
	Value any
}

// Write stores messages in the outbox within tx.
//
// The caller's trace context is stored with each row, so the relay's produce
// joins the trace of the request that caused it rather than starting a new
// one — a trip completed at 12:00:00 and relayed 80ms later is still one
// trace.
func Write(ctx context.Context, tx pgx.Tx, table Table, messages ...Message) error {
	if len(messages) == 0 {
		return nil
	}

	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	headers, err := json.Marshal(carrier)
	if err != nil {
		return fmt.Errorf("outbox: encode headers: %w", err)
	}

	for _, message := range messages {
		value, err := json.Marshal(message.Value)
		if err != nil {
			return fmt.Errorf("outbox: encode %s: %w", message.Topic, err)
		}
		if _, err := tx.Exec(ctx,
			`insert into `+table.name+` (topic, key, value, headers) values ($1, $2, $3, $4)`,
			message.Topic, message.Key, value, headers,
		); err != nil {
			return fmt.Errorf("outbox: write %s: %w", table.name, err)
		}
	}
	return nil
}

// Hooks are the observability seams.
type Hooks struct {
	// OnPublished reports a flushed batch and the age of its oldest row, which
	// is the relay's lag: how long a committed fact waited to be heard.
	OnPublished func(count int, oldest time.Duration)
	// OnError reports a flush that failed and will be retried.
	OnError func(err error)
}

// Options tune the relay.
type Options struct {
	// Batch is the most rows published per transaction. Default 500.
	Batch int
	// Interval is how long an idle relay waits before looking again. Default
	// 100ms. A full batch is followed immediately by another, so this bounds the
	// latency of a quiet outbox, not the throughput of a busy one.
	Interval time.Duration
	Hooks    Hooks
	// Now is injectable for tests.
	Now func() time.Time
}

// Relay moves committed rows from an outbox table to Kafka.
type Relay struct {
	pool     *pgxpool.Pool
	table    Table
	producer *kgo.Client
	options  Options
}

func NewRelay(pool *pgxpool.Pool, table Table, producer *kgo.Client, options Options) *Relay {
	if options.Batch <= 0 {
		options.Batch = 500
	}
	if options.Interval <= 0 {
		options.Interval = 100 * time.Millisecond
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Relay{pool: pool, table: table, producer: producer, options: options}
}

// Run relays until ctx is cancelled.
func (r *Relay) Run(ctx context.Context) error {
	for {
		published, err := r.Flush(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil && r.options.Hooks.OnError != nil {
			r.options.Hooks.OnError(err)
		}

		// A full batch means there is probably more waiting; anything less
		// means the table is drained and the relay can rest.
		if err == nil && published == r.options.Batch {
			continue
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(r.options.Interval):
		}
	}
}

type row struct {
	id        int64
	topic     string
	key       string
	value     []byte
	headers   propagation.MapCarrier
	createdAt time.Time
}

// Flush publishes one batch and returns how many rows it moved.
//
// One relay publishes at a time, across every instance of the service, by
// holding a transaction-scoped advisory lock on the table's name. That is what
// keeps per-key order: two relays each taking half of a batch would publish a
// trip's "completed" and "cancelled" in whichever order their brokers
// answered. Within one relay, rows go out in id order, and two changes to the
// same trip cannot commit out of id order because the second one's UPDATE
// waits on the first one's row lock before it can write its outbox row.
func (r *Relay) Flush(ctx context.Context) (int, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("outbox: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	var leader bool
	if err := tx.QueryRow(ctx, `select pg_try_advisory_xact_lock(hashtext($1))`, r.table.name).Scan(&leader); err != nil {
		return 0, fmt.Errorf("outbox: lock: %w", err)
	}
	if !leader {
		return 0, nil
	}

	rows, err := r.read(ctx, tx)
	if err != nil || len(rows) == 0 {
		return 0, err
	}

	if err := r.publish(ctx, rows); err != nil {
		// Nothing is deleted, so every row in the batch is retried — including
		// any the broker did accept. That duplicate is the at-least-once the
		// package comment promises consumers.
		return 0, err
	}

	ids := make([]int64, len(rows))
	for i, row := range rows {
		ids[i] = row.id
	}
	if _, err := tx.Exec(ctx, `delete from `+r.table.name+` where id = any($1)`, ids); err != nil {
		return 0, fmt.Errorf("outbox: delete: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("outbox: commit: %w", err)
	}

	if r.options.Hooks.OnPublished != nil {
		r.options.Hooks.OnPublished(len(rows), r.options.Now().Sub(rows[0].createdAt))
	}
	return len(rows), nil
}

func (r *Relay) read(ctx context.Context, tx pgx.Tx) ([]row, error) {
	result, err := tx.Query(ctx,
		`select id, topic, key, value, headers, created_at from `+r.table.name+` order by id limit $1`,
		r.options.Batch)
	if err != nil {
		return nil, fmt.Errorf("outbox: read: %w", err)
	}
	defer result.Close()

	var rows []row
	for result.Next() {
		var (
			next    row
			headers []byte
		)
		if err := result.Scan(&next.id, &next.topic, &next.key, &next.value, &headers, &next.createdAt); err != nil {
			return nil, fmt.Errorf("outbox: scan: %w", err)
		}
		if err := json.Unmarshal(headers, &next.headers); err != nil {
			return nil, fmt.Errorf("outbox: decode headers of row %d: %w", next.id, err)
		}
		rows = append(rows, next)
	}
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("outbox: read: %w", err)
	}
	return rows, nil
}

// publish produces every row and waits for all of them to be acknowledged.
func (r *Relay) publish(ctx context.Context, rows []row) error {
	var (
		wait  sync.WaitGroup
		mu    sync.Mutex
		first error
	)

	for _, row := range rows {
		// The writer's trace, restored, so this produce is a child of the
		// request that committed the row.
		recordCtx := otel.GetTextMapPropagator().Extract(ctx, row.headers)

		wait.Add(1)
		tracing.Produce(recordCtx, r.producer, &kgo.Record{
			Topic: row.topic,
			Key:   []byte(row.key),
			Value: row.value,
		}, func(_ *kgo.Record, err error) {
			defer wait.Done()
			if err != nil {
				mu.Lock()
				if first == nil {
					first = fmt.Errorf("outbox: produce %s: %w", row.topic, err)
				}
				mu.Unlock()
			}
		})
	}

	wait.Wait()
	return first
}
