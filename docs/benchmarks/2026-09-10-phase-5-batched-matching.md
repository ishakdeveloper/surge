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

### Found afterwards: a clean handover replayed its last seconds

Greedy's four redelivered matches were one event, not four. Each trip was
offered once and matched to one driver; the second `TripMatched` for all four
came from the _batched_ run's matcher in the millisecond it restored its
partitions, three seconds after greedy's matcher revoked them cleanly.

The cause was in the matcher's consumer, not its matching. franz-go's default
`OnPartitionsRevoked` is a blocking commit of processed offsets; the runner
replaces it with its own callback to write checkpoints, and that callback never
committed. Whatever a shard had processed since the last 5s autocommit was
replayed by the next owner: the rider's `MatchRequested` recreated the pending
request, and the other shard's `ReserveResult: OK` matched it again, to the same
driver. The poll loop also marked records for commit as it _queued_ them for a
worker rather than once the worker had handled them, which could commit records
nobody processed and lose them at a handover.

Workers now mark a batch only after handling it, and revoke and shutdown commit
synchronously after the checkpoint is flushed. `make check-handover` measures
it: a fleet and a trickle of riders, then a clean stop the moment `geo.events`
goes quiet, then the group's committed offsets.

|        | partitions lagging | records processed but not committed |
| ------ | -----------------: | ----------------------------------: |
| before |   31 / 32, 27 / 32 |                            280, 277 |
| after  |     0 / 32, 0 / 32 |                                0, 0 |

The trip service applies a match once however often it hears of it, so nobody
was ever sent two cars — but every handover was doing seconds of work twice.

## Round two: cheaper batches, and cars that are actually scarce

The first valid run gave batching two handicaps. It waited on Valhalla for every
window, contested or not, and it ran against a fleet that never got busy. Both
changed:

- **An uncontested window skips the solve.** If no car is a candidate for more
  than one rider, a joint assignment cannot beat each rider's nearest car, so
  the window dispatches at once without asking the router. One rider alone is
  the common case of this.
- **The router has 400 ms, not three seconds**, and a shard gives up on an
  answer after one second.
- **Drivers do their trips** (`SIM_TRIP_KM=1.5`): an accepted driver drives to
  the pickup and carries the rider about 1.5 km before cruising again, so supply
  drains the way a real fleet's does.
- **Demand has hotspots** (`SIM_HOTSPOT_SHARE=0.7`): seven pickups in ten land
  within 800 m of Centraal, Zuid or Leidseplein, so riders compete for the same
  cars.
- **Warm-up is three minutes**, long enough for a fleet whose trips take three
  and a half to settle into being busy.

300 drivers, 2 requests/s, 4 minutes measured per strategy. Both valid; neither
matcher lost a partition.

|                             |  greedy |    batched |
| --------------------------- | ------: | ---------: |
| **double dispatches**       |   **0** |      **0** |
| redelivered matches         |       0 |          0 |
| riders matched / s          |    0.50 |       0.50 |
| riders unmatched / s        |    1.51 |       1.51 |
| road pickup, mean           | 229.1 s |    254.1 s |
| road pickup, p50            | 181.2 s |    239.7 s |
| road pickup, p95            | 640.4 s |    564.2 s |
| straight-line pickup, mean  |   683 m |      680 m |
| match latency p50           |  1.56 s |     4.04 s |
| match latency p99           |  4.80 s |     7.95 s |
| contested solves            |       – | 7 in 4 min |
| riders per contested solve  |       – |       2.14 |
| solves without travel times |       – |          0 |

**Batching is now cheap, and still not better.** Median latency fell from 9.5 s
to 4.0 s and no solve waited out a router. But in four minutes only seven
windows held two riders wanting the same car; every other decision was
greedy's, taken two seconds later. The match rate is identical, and the pickup
times sit within noise of each other — 162 priced pickups a side, a standard
error around 12 s, the median favouring greedy and the p95 batching. The wait
is paid on every request; the benefit arrives only on the rare contested one.

For batching to win, a shard's window has to hold riders who compete: far more
demand per shard than one laptop's fleet produces, or a longer window, which
costs latency again. At this scale greedy is the right default, and the batch
path stays for the day the numbers change.

The table above ran as two invocations: the memory guard killed the
paired run halfway through its second half, so `STRATEGIES=batched` now runs
one alone, with the same settings. Getting here also took one more harness fix.
A second attempt reused the consumer group `matcher-bench` from an earlier
invocation, so its greedy run resumed hours back and replayed every request
since — 806 matches and 3,544 unmatched in a window that had seen about 850
real requests. Each invocation now gets a fresh group.

### Run again, in one piece

Launched detached — so nothing that can be killed for memory owns it — and with
the dashboards stopped for its length, the paired run finished, same settings:

|                            |  greedy | batched |
| -------------------------- | ------: | ------: |
| **double dispatches**      |   **0** |   **0** |
| redelivered matches        |       0 |       0 |
| riders unmatched / s       |    1.51 |    1.53 |
| road pickup, mean          | 261.2 s | 244.1 s |
| road pickup, p50           | 249.6 s | 196.7 s |
| road pickup, p95           | 616.1 s | 575.2 s |
| straight-line pickup, mean |   644 m |   768 m |
| match latency p50          |  1.65 s |  4.12 s |
| match latency p99          |  4.97 s |  7.77 s |
| contested solves           |       – |       0 |

This time batching's mean pickup is the shorter one. Across the two runs of the
same settings, greedy's mean went from 229 s to 261 s and batching's from 254 s
to 244 s: the run-to-run swing is as large as the gap between the strategies, so
neither direction means anything. And this run solved no contested window at
all. What does not move between runs is the cost — batching offers about two
and a half seconds later and matches the same riders. The conclusion stands on
two runs rather than one.

### Three more runs, and what the repeats say

The two runs above disagreed about which strategy picked riders up faster, so
the comparison was repeated three times back to back after a restart, same
settings, each run launched detached. All six halves were valid; no double
dispatch, no redelivered match, no lost partition.

| run | load at start | road mean greedy | batched | road p50 greedy | batched | road p95 greedy | batched | matched/s greedy | batched | latency p50 greedy | batched | contested solves |
| --- | ------------: | ---------------: | ------: | --------------: | ------: | --------------: | ------: | ---------------: | ------: | -----------------: | ------: | ---------------: |
| 1   |            23 |            254 s |   224 s |           212 s |   144 s |           668 s |   522 s |             0.45 |    0.46 |             1.62 s |  3.98 s |                8 |
| 2   |           122 |            271 s |   224 s |           215 s |   182 s |           721 s |   543 s |             0.38 |    0.46 |             1.62 s |  3.84 s |                7 |
| 3   |             6 |            249 s |   271 s |           231 s |   227 s |           682 s |   737 s |             0.39 |    0.45 |             1.62 s |  3.89 s |                3 |

Run 2 started while an iOS Simulator was booting on the same machine — a load
average of 122 — and run 1 at 23, before the machine had settled after the
restart. Only run 3 started quiet. Its halves are the ones to trust most, and
they are the ones that favour greedy.

Across all five paired comparisons — these three and the two above — batching's
mean pickup was -9 s against greedy's, with a standard error of
14 s. That is not a difference: the runs disagree with each other more
than the strategies do. Two things held in every run instead:

- **Batching offers about 2.3 seconds later** — the window, every time.
- **Batching matched a few more riders today**, 0.46 a second against
  0.41, in all three runs — but not in the two runs before, so it is a
  lead, not a finding.

The conclusion is the same one, now on five runs rather than one: at this
scale the joint solve rarely has anything to decide, its pickup benefit is
inside the noise, and its latency cost is not. Greedy stays the default.

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
