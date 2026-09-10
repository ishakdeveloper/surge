package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/ishakdeveloper/surge/services/matcher/internal/domain"
	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/ishakdeveloper/surge/shared/tracing"
	"github.com/ishakdeveloper/surge/shared/wire"
	"github.com/twmb/franz-go/pkg/kgo"
)

// Hooks are the observability seams, so this package does not import Prometheus.
type Hooks struct {
	OnMatched   func(domain.MatchResult)
	OnAbandoned func()
	OnRejection func(wire.ReserveRejection)
	OnEvent     func(tag string)
	// OnOwnership reports the TOTAL partitions held after a change, not the
	// delta. A cooperative rebalance moves a subset, so reporting the size of
	// the latest assignment makes the gauge read as a loss every time a few
	// partitions come back.
	OnOwnership      func(total int, changed []int32, gained bool)
	OnRestoreStall   func(partition int32, duration time.Duration, offers int)
	OnShardState     func(partition int32, drivers, pending, offers int)
	OnHandlerError   func(error)
	OnProduceFailure func(error)
}

// Runner owns the consumer group and one worker per assigned partition.
//
// The whole point of the structure: franz-go hands out partitions, and each one
// gets a goroutine with its own domain.Shard. Nothing is shared between them, so the
// "single writer per geography" property is a consequence of the process
// layout rather than something enforced by discipline.
type Runner struct {
	brokers  []string
	config   domain.Config
	hooks    Hooks
	producer *kgo.Client

	mu      sync.Mutex
	workers map[int32]*worker
}

type worker struct {
	shard   *domain.Shard
	records chan []*kgo.Record
	// stopped asks the goroutine to leave.
	stopped chan struct{}
	// done is the goroutine having left. A checkpoint reads the shard, and
	// reading it while run may still be mid-event writes down a torn state —
	// closing stopped alone does not wait for anything.
	done chan struct{}
	// restored closes once the checkpoint has been applied and run has
	// started. Until then this instance has learned nothing the previous
	// owner's checkpoint does not already say, so there is nothing to write on
	// the way out — and writing this still-empty shard would overwrite that
	// checkpoint and erase every reservation in it.
	restored chan struct{}
}

func NewRunner(brokers []string, config domain.Config, producer *kgo.Client, hooks Hooks) *Runner {
	return &Runner{
		brokers:  brokers,
		config:   config,
		hooks:    hooks,
		producer: producer,
		workers:  make(map[int32]*worker),
	}
}

// Run consumes until ctx is cancelled, then checkpoints everything it still
// owns so a clean shutdown loses no promises.
func (r *Runner) Run(ctx context.Context, group string) error {
	client, err := kafkax.NewShardConsumer(r.brokers, group, []string{kafkax.TopicGeoEvents},
		kgo.OnPartitionsAssigned(r.assigned),
		kgo.OnPartitionsRevoked(r.revoked),
		// Lost differs from revoked: the session died, so there is no
		// opportunity to checkpoint and no point pretending otherwise. The
		// offers stranded by it expire on their own deadlines, which is exactly
		// why every hold has one.
		kgo.OnPartitionsLost(r.lost),
	)
	if err != nil {
		return err
	}
	defer client.Close()

	for {
		if ctx.Err() != nil {
			r.shutdown(context.WithoutCancel(ctx))
			return nil
		}

		fetches := client.PollRecords(ctx, 20_000)
		if fetches.IsClientClosed() {
			r.shutdown(context.WithoutCancel(ctx))
			return nil
		}

		fetches.EachError(func(topic string, partition int32, err error) {
			if ctx.Err() == nil {
				slog.Warn("fetch error", "topic", topic, "partition", partition, "error", err)
			}
		})

		fetches.EachPartition(func(batch kgo.FetchTopicPartition) {
			if len(batch.Records) == 0 {
				return
			}

			r.mu.Lock()
			target, ok := r.workers[batch.Partition]
			r.mu.Unlock()

			if !ok {
				// Records for a partition we have already given up. Dropping
				// them is correct — the new owner is consuming them too.
				return
			}

			select {
			case target.records <- batch.Records:
			case <-target.stopped:
			}
		})

		// Marked, not committed: the auto-committer advances only past records
		// a worker has actually processed.
		client.MarkCommitRecords(collect(fetches)...)
	}
}

func collect(fetches kgo.Fetches) []*kgo.Record {
	var records []*kgo.Record
	fetches.EachRecord(func(record *kgo.Record) { records = append(records, record) })
	return records
}

// assigned takes ownership at once and restores in the background.
//
// The restore used to run here, one partition at a time, and it could not stay.
// This callback runs inside the group protocol, franz-go documents that it must
// not outlast the rebalance interval, and the session is ten seconds. Each
// restore opened two new Kafka clients and looked up offsets for the whole
// topic, so thirty-two of them against a slow broker took minutes: the member
// was evicted mid-restore, rejoined, was handed the same partitions and began
// again — owning nothing, indefinitely, while riders waited for a driver.
//
// "Restore before serving" still holds; it is enforced by fetching rather than
// by blocking. The partitions are paused before this returns — fetches for newly
// assigned partitions only begin after it does — and each resumes once its own
// shard has been restored and its worker started. No record reaches a shard
// that does not yet know which drivers it has promised.
func (r *Runner) assigned(ctx context.Context, client *kgo.Client, assigned map[string][]int32) {
	partitions := assigned[kafkax.TopicGeoEvents]
	if len(partitions) == 0 {
		return
	}

	client.PauseFetchPartitions(map[string][]int32{kafkax.TopicGeoEvents: partitions})

	started := time.Now()
	fresh := make(map[int32]*worker, len(partitions))

	r.mu.Lock()
	for _, partition := range partitions {
		w := &worker{
			shard:    domain.NewShard(partition, r.config),
			records:  make(chan []*kgo.Record, 8),
			stopped:  make(chan struct{}),
			done:     make(chan struct{}),
			restored: make(chan struct{}),
		}
		r.workers[partition] = w
		fresh[partition] = w
	}
	r.mu.Unlock()

	// Ownership is reported on assignment, not on restore: the partition is
	// ours from this moment, and the restore stall histogram is what measures
	// the gap before it serves.
	r.reportOwnership(partitions, true)
	slog.Info("partitions assigned, restoring", "partitions", partitions, "owned", r.owned())

	go r.restore(ctx, client, fresh, started)
}

// restore reads every new partition's checkpoint in one pass, then brings each
// shard up and lets its records flow.
func (r *Runner) restore(ctx context.Context, client *kgo.Client, fresh map[int32]*worker, started time.Time) {
	partitions := make([]int32, 0, len(fresh))
	for partition := range fresh {
		partitions = append(partitions, partition)
	}

	records, err := kafkax.ReadCompacted(ctx, r.brokers, kafkax.TopicGeoState, partitions)
	if err != nil {
		// Starting without a checkpoint is worse than starting slowly, but
		// never starting is worse still: the partitions would go unconsumed.
		// The stranded offers expire on their own deadlines.
		slog.Warn("checkpoint restore failed; starting cold", "partitions", partitions, "error", err)
	}

	for partition, w := range fresh {
		select {
		case <-w.stopped:
			// Revoked or lost while restoring: somebody else owns it now, so
			// it is neither started nor resumed.
			continue
		default:
		}

		var checkpoints []wire.ShardCheckpoint
		for _, raw := range records[partition] {
			var checkpoint wire.ShardCheckpoint
			if err := json.Unmarshal(raw, &checkpoint); err == nil {
				checkpoints = append(checkpoints, checkpoint)
			}
		}
		w.shard.Restore(checkpoints, time.Now())

		if r.hooks.OnRestoreStall != nil {
			r.hooks.OnRestoreStall(partition, time.Since(started), w.shard.Offers())
		}

		close(w.restored)
		go r.run(w)
		client.ResumeFetchPartitions(map[string][]int32{kafkax.TopicGeoEvents: {partition}})
	}
}

// revoked is the graceful half of a rebalance: stop, write down the promises,
// let go.
func (r *Runner) revoked(ctx context.Context, client *kgo.Client, revoked map[string][]int32) {
	partitions := revoked[kafkax.TopicGeoEvents]
	if len(partitions) == 0 {
		return
	}

	for _, partition := range partitions {
		r.mu.Lock()
		w, ok := r.workers[partition]
		delete(r.workers, partition)
		r.mu.Unlock()

		if !ok {
			continue
		}

		r.release(ctx, w)
	}

	r.reportOwnership(partitions, false)
	slog.Info("partitions revoked", "partitions", partitions, "owned", r.owned())
}

// lost is the ungraceful half. There is nothing to write: the session is gone,
// so a checkpoint would be a write to a partition somebody else already owns.
func (r *Runner) lost(_ context.Context, _ *kgo.Client, lost map[string][]int32) {
	for _, partition := range lost[kafkax.TopicGeoEvents] {
		r.mu.Lock()
		w, ok := r.workers[partition]
		delete(r.workers, partition)
		r.mu.Unlock()

		if ok {
			close(w.stopped)
		}
	}

	if partitions := lost[kafkax.TopicGeoEvents]; len(partitions) > 0 {
		slog.Warn("partitions lost without a chance to checkpoint", "partitions", partitions)
		r.reportOwnership(partitions, false)
	}
}

// release stops a worker and writes down its promises, if it has any of its own.
func (r *Runner) release(ctx context.Context, w *worker) {
	close(w.stopped)

	select {
	case <-w.restored:
		// Wait for the goroutine to actually leave, so the checkpoint is a
		// consistent snapshot rather than one taken halfway through an event.
		<-w.done
		r.checkpoint(ctx, w.shard)
	default:
		// Still restoring. The previous owner's checkpoint is the truth for
		// this partition, and writing this empty shard over it would erase it.
	}
}

func (r *Runner) owned() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.workers)
}

func (r *Runner) reportOwnership(changed []int32, gained bool) {
	if r.hooks.OnOwnership != nil {
		r.hooks.OnOwnership(r.owned(), changed, gained)
	}
}

// run is one shard's whole life: a single goroutine, and therefore a single
// writer for every cell in this partition.
func (r *Runner) run(w *worker) {
	defer close(w.done)

	// The sweep that expires offers and requests. Frequent enough that a
	// timeout is felt as a timeout rather than as a hang.
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()

	report := time.NewTicker(2 * time.Second)
	defer report.Stop()

	for {
		select {
		case <-w.stopped:
			return

		case batch := <-w.records:
			for _, record := range batch {
				r.handle(w, record)
			}

		case now := <-tick.C:
			// Expiries have no inbound record to inherit a trace from, so they
			// start their own. A timed-out offer is worth seeing as a trace in
			// its own right: it is a rider waiting.
			r.emit(context.Background(), w.shard.Tick(now))

		case <-report.C:
			if r.hooks.OnShardState != nil {
				r.hooks.OnShardState(w.shard.Partition(),
					w.shard.Drivers(), w.shard.Pending(), w.shard.Offers())
			}
		}
	}
}

// handle processes one record inside a span continued from its producer.
func (r *Runner) handle(w *worker, record *kgo.Record) {
	ctx, span := tracing.Consume(context.Background(), record, "matcher.event")
	defer span.End()

	event, err := wire.DecodeGeoEvent(record.Value)
	if err != nil {
		tracing.Fail(span, err)
		if r.hooks.OnHandlerError != nil {
			r.hooks.OnHandlerError(err)
		}
		return
	}

	span.SetName("matcher." + event.Tag)
	span.SetAttributes(
		attribute.String("surge.cell", event.Cell),
		attribute.Int("surge.partition", int(w.shard.Partition())),
	)

	outcome, err := w.shard.Handle(event, time.Now())
	if err != nil {
		tracing.Fail(span, err)
		if r.hooks.OnHandlerError != nil {
			r.hooks.OnHandlerError(err)
		}
		return
	}

	if len(outcome.Matched) > 0 {
		span.SetAttributes(
			attribute.String("surge.trip", outcome.Matched[0].TripID),
			attribute.String("surge.driver", outcome.Matched[0].DriverID),
			attribute.Float64("surge.match_latency_seconds", outcome.Matched[0].Latency.Seconds()),
		)
	}

	if r.hooks.OnEvent != nil {
		r.hooks.OnEvent(event.Tag)
	}
	r.emit(ctx, outcome)
}

// emit sends everything an outcome produced, and counts what happened.
func (r *Runner) emit(ctx context.Context, outcome domain.Outcome) {
	for _, event := range outcome.GeoEvents {
		r.produce(ctx, kafkax.TopicGeoEvents, event.Cell, event)
	}
	for _, offer := range outcome.Offers {
		// The whole envelope, keyed by the driver: the gateway forwards push
		// records verbatim and decides only who receives them.
		r.produce(ctx, kafkax.TopicWSPush, offer.DriverID,
			wire.ServerMessage{Tag: wire.TagOffer, Offer: &offer})
	}

	for _, match := range outcome.Matched {
		// Keyed by trip id on its own topic, so the trip service consumes the
		// handful of outcomes it cares about rather than ten thousand position
		// updates a second looking for them.
		r.produce(ctx, kafkax.TopicTripEvents, match.TripID, wire.TripEvent{
			Tag:    wire.TagTripMatched,
			TripID: match.TripID,
			AtMs:   time.Now().UnixMilli(),
			Matched: &wire.TripMatchedPayload{
				DriverID:  match.DriverID,
				LatencyMs: match.Latency.Milliseconds(),
			},
		})

		if r.hooks.OnMatched != nil {
			r.hooks.OnMatched(match)
		}
	}
	for _, tripID := range outcome.Abandoned {
		r.produce(ctx, kafkax.TopicTripEvents, tripID, wire.TripEvent{
			Tag:       wire.TagTripUnmatched,
			TripID:    tripID,
			AtMs:      time.Now().UnixMilli(),
			Unmatched: &wire.TripUnmatchedPayload{Reason: "no_drivers"},
		})

		if r.hooks.OnAbandoned != nil {
			r.hooks.OnAbandoned()
		}
	}
	for _, rejection := range outcome.Rejections {
		if r.hooks.OnRejection != nil {
			r.hooks.OnRejection(rejection)
		}
	}
}

func (r *Runner) produce(ctx context.Context, topic, key string, message any) {
	payload, err := json.Marshal(message)
	if err != nil {
		if r.hooks.OnProduceFailure != nil {
			r.hooks.OnProduceFailure(err)
		}
		return
	}

	tracing.Produce(ctx, r.producer, &kgo.Record{
		Topic: topic,
		Key:   []byte(key),
		Value: payload,
	}, func(_ *kgo.Record, err error) {
		if err != nil && r.hooks.OnProduceFailure != nil {
			r.hooks.OnProduceFailure(err)
		}
	})
}

func (r *Runner) checkpoint(ctx context.Context, shard *domain.Shard) {
	for _, record := range shard.Checkpoint(time.Now()) {
		payload, err := json.Marshal(record)
		if err != nil {
			continue
		}

		r.producer.Produce(ctx, &kgo.Record{
			Topic: kafkax.TopicGeoState,
			Key:   []byte(record.Cell),
			Value: payload,
		}, nil)
	}

	// Synchronous: the partition is about to belong to somebody else, and a
	// checkpoint still sitting in a producer buffer is a checkpoint that does
	// not exist.
	flush, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := r.producer.Flush(flush); err != nil {
		slog.Warn("checkpoint flush failed", "error", err)
	}
}

func (r *Runner) shutdown(ctx context.Context) {
	r.mu.Lock()
	workers := make([]*worker, 0, len(r.workers))
	for partition, w := range r.workers {
		workers = append(workers, w)
		delete(r.workers, partition)
	}
	r.mu.Unlock()

	for _, w := range workers {
		r.release(ctx, w)
	}
}
