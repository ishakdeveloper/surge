package events

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"strconv"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/ishakdeveloper/surge/services/matcher/internal/domain"
	"github.com/ishakdeveloper/surge/shared/geo"
	"github.com/ishakdeveloper/surge/shared/kafkax"
	"github.com/ishakdeveloper/surge/shared/routing"
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
	// OnBatch reports one travel-time fetch for a batched solve: how many
	// riders and cars it covered, how long the router took, and whether it
	// answered at all.
	OnBatch func(requests, drivers int, took time.Duration, err error)
	// OnDispatched reports every offer: which car, which rider, how far apart.
	// Called from every worker goroutine at once.
	OnDispatched func(domain.Dispatch)
}

// Router is where batched matching gets its travel times.
type Router interface {
	Matrix(ctx context.Context, sources, targets []geo.Point) ([][]routing.MatrixCell, error)
}

// Runner owns the consumer group and one worker per assigned partition.
//
// The whole point of the structure: franz-go hands out partitions, and each one
// gets a goroutine with its own domain.Shard. Nothing is shared between them, so the
// "single writer per geography" property is a consequence of the process
// layout rather than something enforced by discipline.
type Runner struct {
	router   Router
	brokers  []string
	instance string
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

	// mark records a batch as processed, so the autocommit may advance past
	// it. Called by the worker after handling, never by the poll loop on
	// handing over: a record marked while still queued here would be committed
	// before it was processed, and lost if the partition moved in between.
	mark func(...*kgo.Record)

	// solved carries travel times back from Runner.solve. Buffered for the one
	// batch a shard has in flight, so the fetch never waits on the loop.
	solved chan domain.BatchResult

	// latencies and abandoned accumulate between frames. Only the worker
	// goroutine touches them, like the shard itself.
	latencies []int64
	abandoned int
}

func NewRunner(brokers []string, config domain.Config, producer *kgo.Client, router Router, instance string, hooks Hooks) *Runner {
	return &Runner{
		router:   router,
		instance: instance,
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
			r.shutdown(context.WithoutCancel(ctx), client)
			return nil
		}

		fetches := client.PollRecords(ctx, 20_000)
		if fetches.IsClientClosed() {
			r.shutdown(context.WithoutCancel(ctx), client)
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

	}
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
			solved:   make(chan domain.BatchResult, 1),
			mark:     client.MarkCommitRecords,
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
	r.commit(ctx, client)

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

	// The console's view of this partition. Once a second: fast enough that a
	// map looks live, slow enough that thirty-two partitions cost a few
	// kilobytes a second rather than a stream the size of the pings.
	frame := time.NewTicker(time.Second)
	defer frame.Stop()

	for {
		select {
		case <-w.stopped:
			return

		case batch := <-w.records:
			for _, record := range batch {
				r.handle(w, record)
			}
			w.mark(batch...)

		case now := <-tick.C:
			// Expiries have no inbound record to inherit a trace from, so they
			// start their own. A timed-out offer is worth seeing as a trace in
			// its own right: it is a rider waiting.
			r.emit(context.Background(), w, w.shard.Tick(now))

		case result := <-w.solved:
			// A batch's travel times, back from the router. Like an expiry it
			// has no inbound record to continue a trace from.
			r.emit(context.Background(), w, w.shard.Solved(result, time.Now()))

		case <-frame.C:
			r.publishFrame(w)

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
	r.emit(ctx, w, outcome)
}

// emit sends everything an outcome produced, and counts what happened.
func (r *Runner) emit(ctx context.Context, w *worker, outcome domain.Outcome) {
	for _, event := range outcome.GeoEvents {
		r.produce(ctx, kafkax.TopicGeoEvents, event.Cell, event)
	}
	for _, offer := range outcome.Offers {
		// The whole envelope, keyed by the driver: the gateway forwards push
		// records verbatim and decides only who receives them.
		r.produce(ctx, kafkax.TopicWSPush, offer.DriverID,
			wire.ServerMessage{Tag: wire.TagOffer, Offer: &offer})
	}

	if r.hooks.OnDispatched != nil {
		for _, dispatch := range outcome.Dispatches {
			r.hooks.OnDispatched(dispatch)
		}
	}

	for _, match := range outcome.Matched {
		w.latencies = append(w.latencies, match.Latency.Milliseconds())

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
		w.abandoned++

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
	if outcome.Batch != nil {
		r.solve(w, *outcome.Batch)
	}
}

// solve fetches the travel times a batch asked for, off the worker's
// goroutine, and hands them back to it.
//
// The only I/O in matching, and the reason it lives here rather than in the
// shard: a matrix call takes tens of milliseconds, and the loop that owns a
// slice of the city must not stop for it. The shard keeps handling pings,
// replies and new requests meanwhile, and the answer arrives as one more
// message on the worker's queue. The deadline sits inside the shard's own
// BatchTimeout, so a slow router is given up on here before the shard gives
// up on it there.
func (r *Runner) solve(w *worker, request domain.BatchRequest) {
	go func() {
		started := time.Now()
		result := domain.BatchResult{ID: request.ID}
		if r.router == nil {
			result.Err = errors.New("matcher: no router configured")
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), r.config.BatchTimeout*3/5)
			matrix, err := r.router.Matrix(ctx, request.Drivers, request.Pickups)
			cancel()
			result.Err = err
			if err == nil {
				result.Seconds = travelSeconds(matrix)
			}
		}
		if r.hooks.OnBatch != nil {
			r.hooks.OnBatch(len(request.Pickups), len(request.Drivers), time.Since(started), result.Err)
		}
		select {
		case w.solved <- result:
		case <-w.stopped:
		}
	}()
}

// travelSeconds turns the router's matrix into the shard's costs, with +Inf
// where there is no road: a pair the solver must never choose, not a free one.
func travelSeconds(matrix [][]routing.MatrixCell) [][]float64 {
	out := make([][]float64, len(matrix))
	for j, row := range matrix {
		out[j] = make([]float64, len(row))
		for i, cell := range row {
			if cell.Reachable {
				out[j][i] = cell.Duration.Seconds()
			} else {
				out[j][i] = math.Inf(1)
			}
		}
	}
	return out
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

func (r *Runner) shutdown(ctx context.Context, client *kgo.Client) {
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
	r.commit(ctx, client)
}

// commit writes down, synchronously, how far each released shard got.
//
// After the checkpoint, never before. The checkpoint is the shard's state as of
// its last processed record, and the offsets committed here are exactly the
// records that state includes, so the next owner restores one and resumes from
// the other and neither replays nor skips anything.
//
// franz-go's default OnPartitionsRevoked is itself a blocking commit; this
// runner replaces it, and for a long time did not commit at all. A shard's last
// few seconds of work were committed only if the 5s autocommit happened to land
// first, and the next owner did them again: a benchmark saw four trips matched
// a second time, to the same driver, in the millisecond a new matcher restored.
func (r *Runner) commit(ctx context.Context, client *kgo.Client) {
	commit, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.CommitMarkedOffsets(commit); err != nil {
		slog.Warn("commit on release failed", "error", err)
	}
}

// publishFrame tells the console what this partition holds.
//
// Keyed by partition, so each gateway's view of a partition is simply the
// latest record for it. The counters reset here, which is what makes them
// "since the last frame".
func (r *Runner) publishFrame(w *worker) {
	drivers, pending, offers := w.shard.Frame()

	latencies := w.latencies
	if latencies == nil {
		latencies = []int64{}
	}

	frame := wire.FleetFrame{
		Tag:              wire.TagFleetFrame,
		Partition:        w.shard.Partition(),
		Instance:         r.instance,
		AtMs:             time.Now().UnixMilli(),
		Drivers:          drivers,
		Pending:          pending,
		Offers:           offers,
		MatchLatenciesMs: latencies,
		Abandoned:        w.abandoned,
	}
	w.latencies = nil
	w.abandoned = 0

	r.produce(context.Background(), kafkax.TopicFleetFrames, strconv.Itoa(int(frame.Partition)), frame)
}
