# Phase 1 — location ingest, and the first thing that broke

**Date** 2026-09-10 · **Machine** Apple silicon, 10 cores, 16 GB (Docker VM 8 GB)
**Under test** `simd` → Kafka `loc.ping` → `ingest` in-memory H3 index
**Everything on the host**; Redpanda, Postgres, Redis and Valhalla in Docker.

## The knob

Ingest rate is `drivers ÷ ping interval`. Both are settable, and they are not
interchangeable — more drivers grows the index, a shorter interval grows the
message rate. Every run below holds the interval at 4 s and moves fleet size,
so the two effects stay coupled the way they would be in a real city.

## Results

Ping age is measured from the moment the driver emitted the ping to the moment
it landed in the index — not the Kafka timestamp. It is how stale the map is,
which is the number a rider would feel. Consumer lag says something different
and, as it turned out, less useful.

|    Drivers |       Target |     Observed |   age p50 |   age p99 | Stale | simd RSS | ingest RSS |
| ---------: | -----------: | -----------: | --------: | --------: | ----: | -------: | ---------: |
|      5,000 |      1,250/s |      1,249/s |     10 ms |     25 ms |     0 |        — |          — |
|     10,000 |      2,500/s |      2,526/s |     10 ms |     25 ms |     0 |        — |          — |
|     20,000 |      5,000/s |      5,013/s |     10 ms |     25 ms |     0 |        — |          — |
| **40,000** | **10,000/s** | **10,040/s** | **10 ms** | **50 ms** | **0** |   332 MB |      66 MB |
|     60,000 |     15,000/s |     15,071/s |     10 ms |     25 ms |     0 |   318 MB |      61 MB |
|     80,000 |     20,000/s |     19,590/s |     10 ms |    250 ms |     0 |   332 MB |      66 MB |
|    100,000 |     25,000/s |   35,672/s ¹ |     10 ms |       5 s |     0 |   422 MB |      67 MB |

¹ Above target because the consumer was draining a backlog, not keeping pace.
That figure is catch-up throughput, not steady state.

**The 10,000 writes/sec target is met at 40,000 drivers with a p99 of 50 ms and
no dropped or out-of-order pings.** Comfortably: it holds to 15,000/s before
anything moves, and degrades from about 20,000/s.

## What broke, and what it was not

Three findings, in the order they were wrong.

### 1. Telemetry was the bottleneck before anything else was

The first ramp fell over at 20,000 drivers: p99 went from 25 ms to over 30
seconds and 5,609 pings were rejected as stale.

The stale count looked like an ordering bug. It was an observer effect. The
Prometheus gauges were updated at the bottom of the poll loop, and
`Index.Stats()` walks every driver to count distinct shards — so at 20,000
drivers and a few hundred polls a second, the consumer was spending several
million map operations a second entirely on measuring itself. It then missed
its 10 s session timeout, the group rebalanced, and Kafka redelivered from the
last commit. The redelivered records had lower sequence numbers than the index
already held, so they were correctly rejected as stale.

The sequence-number check did its job. It was reporting a real symptom of a
problem somewhere else entirely.

Gauges now sample on a one-second ticker. Every number in the table above is
from after that fix.

### 2. Replaying the log on startup was wrong, not just slow

The next run showed _more_ drivers in the index than the simulator was running,
and tens of thousands of stale pings. A restarted consumer group was replaying
six hours of retained pings.

The fix is a design decision rather than a benchmark convenience: `ingest` now
starts at the end of the log. A position is worth something for about as long
as it takes the next ping to arrive, so replaying history spends minutes
rebuilding where drivers _used to be_, produces a live-looking index full of
ghosts, and is overwritten within one ping interval anyway. The index is
derived state with a four-second rebuild time — the honest recovery strategy is
to wait four seconds.

This is the same reasoning that will keep driver positions out of the matcher's
compacted checkpoint in Phase 2, and it is better to have found it here.

### 3. At the top of the range, the load generator is the thing that breaks

At 100,000 drivers, `simd` sat at 100.9 % CPU — one saturated core — while
`ingest` idled at 29 %. The system under test was not the limit.

The control run separates the two. With the discard transport, the same 100,000
drivers hit **25,112 pings/sec against a 25,000/sec target at 37.2 % CPU**: the
driver model itself — 100,022 goroutines, route walking, GPS jitter — is cheap
and keeps time exactly. The missing ~64 % of a core is entirely the produce
path: `json.Marshal` plus `kgo.Produce`, per ping.

A benchmark without that control run would have reported "the system tops out
around 20,000/sec" and been wrong about which system.

Indexing itself costs **3.96 µs per ping** (12.70 s over 3,205,509 pings), and
100,000 drivers occupy **67 MB** of index. Both leave substantial headroom.

## What this says about the next phase

- The in-memory index is emphatically the right call. 3.96 µs and 67 MB for
  100k drivers; a Postgres round trip per ping at 10k/sec is not a close call.
- Geographic sharding is balanced enough to be worth doing: 100,000 drivers
  spread over **133 resolution-7 shard cells** and ~2,100 index cells, so no
  single shard owns anything like a hot majority.
- Cell transitions run at roughly **0.8 per driver per minute** at cruising
  speed. That is the rate at which Phase 2's shards will hand drivers over to
  one another, and it is high enough that the handoff path needs to be cheap.
- The next optimisation is the produce path, not the consumer. That makes the
  JSON-versus-binary question in `pkg/wire` a measurable experiment with a
  known baseline rather than a matter of taste.

## Reproducing

```bash
docker compose -f deploy/compose/docker-compose.yml up -d
cd backend && go build -o bin/simd ./cmd/simd && go build -o bin/ingest ./cmd/ingest
set -a; source ../.env; set +a

INGEST_GROUP="ingest-bench-$(date +%s)" ./bin/ingest &   # a fresh group per run
SIM_DRIVERS=1000 ./bin/simd &

curl -sX POST localhost:8101/sim/config -H 'content-type: application/json' -d '{"drivers":40000}'
curl -s localhost:8102/debug/stats
curl -s localhost:9102/metrics | grep surge_ingest_ping_age

# the control: same fleet, pings discarded
SIM_KAFKA=false SIM_DRIVERS=100000 ./bin/simd
```

Use a fresh `INGEST_GROUP` for each benchmark, or the run measures the previous
run's backlog.
