# Phase 5 — batched matching against greedy

**Date** 2026-09-10 · Apple silicon, 10 cores, 16 GB · infrastructure on OrbStack,
capped at 4 CPUs and 4 GB
**Under test** `simd` riders → `geo.events` → **matcher (greedy | batched)** →
`ws.push` → gateway → simulated drivers' sockets → replies → matcher

Greedy matching gives each rider the nearest free car the moment the request
arrives. That is locally right and globally wrong: two riders a street apart,
two cars, and the first rider takes the car the second needed while the other
car drives twice as far. Batched matching gathers a window of requests per
shard and solves them together over Valhalla travel times, minimising the total.

The question is what that trade buys on this system: batching spends up to one
window of latency before an offer goes out, to shorten the drive to the rider.

## How batching fits the shard

A shard is one goroutine over in-memory state, and `Handle` is pure. Travel
times are I/O, so they cannot be fetched inside it:

1. Requests queue for `MATCHER_BATCH_WINDOW` (2s) instead of being offered.
2. On the tick that closes the window the shard re-ranks each rider's candidates
   (two seconds is a hundred metres of driving) and emits a `BatchRequest`: up
   to 32 riders and 64 cars.
3. The runner fetches the drivers × pickups matrix from Valhalla's
   `/sources_to_targets` on its own goroutine, and hands the answer back to the
   worker as one more message.
4. `Solved` runs the Hungarian algorithm over it. Unreachable pairs and cars that
   were not a rider's candidates cost +Inf, and the solve maximises riders
   matched before minimising their total time.
5. Each assigned car becomes that rider's _first candidate_, and dispatch goes
   through `advance` — the same path greedy uses, which re-checks the car is
   still free before reserving it. A batch decides who to try first; it never
   goes around the checks. A car taken during the solve is skipped exactly as a
   greedy retry would skip it.

If the router fails or takes longer than 3s, the batch is solved over
straight-line distance instead, and a shard gives up on an answer after 5s. A
batch never holds its riders hostage to Valhalla.

## Method

`make bench-matching DRIVERS=300 RPS=20 MINUTES=4`, which for each strategy:

- restarts the matcher with `MATCH_STRATEGY` set, and waits for all 32
  partitions to restore;
- starts 300 drivers on real WebSockets and riders at 20 requests/s, accept
  rate 0.7, 1.5s to answer;
- warms up for 60s, then measures 4 minutes through Prometheus.

Both runs share one consumer group, so the second resumes where the first
stopped. The riders' generator is seeded, so both strategies see the same
pickups in the same order.

A run only counts if it is **valid**: the matcher handled at least 90% of the
offered requests, and no partition moved during the window. Double dispatch is
counted from `trip.events` — at most one `TripMatched` per trip — not from the
simulator's own detector (see below).

**Pickup time** is what is compared, over real roads. The matcher logs every
offer's car and rider positions (`MATCHER_DISPATCH_LOG`), and after both runs
the harness prices a seeded sample of 400 per strategy with Valhalla's
`/route`. After, not during: during a batched run Valhalla is busy with the
matcher's own matrices, and pricing then would slow the thing being measured.
Straight-line distance is reported too, but it cannot judge the comparison —
it is exactly what greedy minimises, and in a city of canals and one-way
streets it is not what a rider waits for.

## Result

300 drivers, 20 requests/s, 4 minutes measured per strategy after 60s of
demand. Both runs valid: each handled at least 90% of the offered load and kept
its partitions.

|                                   |  greedy | batched |
| --------------------------------- | ------: | ------: |
| **double dispatches**             |   **0** |   **0** |
| redelivered matches (same driver) |       4 |       0 |
| requests handled / s              |   20.01 |   18.85 |
| road pickup, mean                 | 211.4 s | 211.4 s |
| road pickup, p50                  | 162.4 s | 164.7 s |
| road pickup, p95                  | 538.7 s | 608.4 s |
| straight-line pickup, mean        |   521 m |   581 m |
| match latency p50                 |  1.72 s |  9.47 s |
| match latency p95                 |  4.33 s | 19.40 s |
| match latency p99                 |  5.55 s | 37.49 s |
| unmatched / s                     |    5.58 |    7.99 |
| busy refusals                     |     149 |     321 |
| riders per batch                  |       - |    3.23 |
| solves without travel times       |       - |     75% |

Road pickup times are Valhalla's `/route` over a seeded sample of 400 offers
per strategy (393 priced for batched; seven had no road).

**On this system, as configured, batching bought nothing and cost a great
deal.** Mean pickup time over real roads is identical, the tail is worse, and a
rider waits five times longer for an offer and is more often left unmatched.

Three reasons, in order of weight:

1. **Valhalla is the bottleneck, and batching per shard multiplies the calls.**
   Thirty-two shards each asking for a matrix every two seconds is about sixteen
   requests a second. One Valhalla on two CPUs answered a median matrix in
   0.78s, so 75% of solves timed out at 3s and fell back to straight-line
   distance — a batch then pays the window _and_ the timeout, and still solves
   over the metric greedy already uses.
2. **There was little to solve.** 3.2 riders per batch, demand spread evenly
   over the city, and drivers who never go on a trip: the contention a joint
   solve exploits barely exists here.
3. **Waiting has its own cost.** Cars move and riders give up during the two
   to five seconds a batch spends gathering and fetching; the higher unmatched
   rate and busy refusals are that time showing up.

What would have to change before batching can win, and what to measure next:

- One matrix per matcher instance per window rather than one per shard — about
  one call a second instead of sixteen — or a local travel-time estimate
  instead of a router call on the matching path at all.
- Skip the solve when a window holds a single rider, and fall back after
  hundreds of milliseconds rather than three seconds.
- A regime with real contention: drivers busy for the length of a trip, and
  demand concentrated in hotspots.

`MATCH_STRATEGY` stays `greedy` by default.

### Open: redelivered matches at a handover

Greedy's four redelivered matches are one event, not four. Each trip was offered
once and matched to one driver; the second `TripMatched` for all four came from
the _batched_ run's matcher in the millisecond it restored its partitions,
19:46:39.176, three seconds after greedy's matcher revoked them cleanly. So the
restore brought back offers that had already been accepted, and the accept
replies were replayed because their offsets had not been committed. The revoke
path does flush its checkpoint before handing over, so the gap is elsewhere —
in which offsets get committed on revoke, or which cells a checkpoint
supersedes. It is harmless downstream, since the trip service applies a match
once, but a clean handover should not replay anything, and this is the next
thing to chase in the matcher.

## The first attempt, and why it does not count

The first run reported batching as _worse_: 613 m mean pickup against greedy's
491 m. Nothing in it held up.

- **Greedy barely ran.** At a load average near 60 its matcher lost its session
  with Redpanda to i/o timeouts, lost all 32 partitions without a checkpoint,
  and got them back 86 seconds later. The simulator only started asking at the
  end, after filling its route pool from a slow Valhalla. Greedy saw demand for
  about 100 of its 240 seconds and handled 7.95 requests/s of the 20 offered;
  batched handled 19.9. Part of that load was this benchmark's own author
  timing Redpanda's metrics endpoint during the greedy window — three 20-second
  scrapes on a broker with one reactor thread.
- **Straight-line distance favoured greedy by construction**, as above.
- **Valhalla was starved.** At 1.5 CPUs and two threads, 27% of batched solves
  timed out waiting for travel times and fell back to distance. It now has two
  CPUs and four threads.
- **The simulator flagged three double dispatches.** `trip.events` showed each
  of those trips matched at most once. The flag counts a second offer after a
  driver has accepted, and under Redpanda's stalls an accept arrived after its
  8s offer had expired — the matcher correctly refused it and re-offered the
  trip. The same false positive the Phase 3 note describes, from a different
  cause.
- **A thousand trips "matched twice" in `trip.events`** were two runs sharing
  names: the seeded rider generator numbered trips from one every run. Every
  pair was 107–122 seconds apart, one in each run, none within a run. Trip ids
  now carry the run, which also fixes a real bug: they are idempotency keys,
  and a matcher that outlived a simulator restart would have dropped the new
  run's first requests as duplicates for five minutes.

The harness now waits for demand before warming up, rejects runs that did not
keep up or lost partitions, prices road time afterwards, and counts double
dispatch from the matcher's own record.

## Caveats

- **Supply never drains.** A simulated driver who accepts goes straight back to
  idle; nobody is ever on a trip. Contention comes only from reservations held
  while a driver takes 1.5s to answer — much less than a real city at rush hour,
  where the cars a batch could reassign are exactly the scarce ones.
- **Demand is uniform.** Pickups are drawn evenly over the city, so a 2s window
  on one of 32 shards holds few riders. That is the regime where batching has
  the least to work with; the riders-per-batch column says how little.
- **Redpanda was CPU-starved.** During the run its single reactor stalled for
  hundreds of milliseconds at a time, and a full metrics scrape took 11–20s. It
  weighs on both strategies' latency alike, but it is not a quiet machine.
- **One run per strategy, on one laptop.** There is no variance estimate; a gap
  of a few percent is inside what a second run could move.

## Reproducing

```sh
make up                     # infrastructure, dashboards included
make build                  # then trip, ingest, gateway on the host
make bench-matching DRIVERS=300 RPS=20 MINUTES=4
```

The Grafana dashboard _Surge — matching: greedy vs batched_ shows the same
series live; every outcome metric carries a `strategy` label.
