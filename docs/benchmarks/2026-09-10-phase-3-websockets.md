# Phase 3 — the fleet on real WebSockets

**Date** 2026-09-10 · Apple silicon, 10 cores, 16 GB · infrastructure on OrbStack
**Under test** `simd` → **gateway (WebSocket)** → `loc.ping` → `ingest` → `geo.events`
→ `matcher` → `ws.push` → gateway → the same sockets

Phase 1 measured the simulator producing straight to Kafka. This is the swap the
`Transport` interface existed for: every simulated driver now holds a real
WebSocket through the real gateway, which is the only way to exercise the
connection registry, the bounded send queues and slow-consumer eviction.

Driver behaviour is unchanged — same routes, speeds, accept rate and thinking
time — so a difference in the numbers is attributable to the transport.

## Result

5,000 drivers, 10 ride requests/sec, all over sockets:

| | |
|---|---:|
| WebSocket connections | 5,000 |
| Deepest send queue | 0 |
| Slow-consumer evictions | 0 |
| Dial failures | 0 |
| Stale / reordered pings | 0 |
| Matched | 723 |
| Abandoned | 50 |
| Offers delivered to sockets | 1,059 |
| Accepted / declined | 724 / 315 |
| Double dispatch | **0** |

A 94% match rate, and the accept/decline split lands on the configured 0.7.

## The cascade that got there

The first WebSocket run looked healthy at 10,000 connections — no evictions, no
backlog — and had **stopped matching entirely**: 3,019 of 3,747 requests
abandoned, and 159 apparent double dispatches. Three separate faults, and only
the last was in the gateway.

### 1. The double-dispatch detector was wrong

A driver decided to accept, waited its thinking time, committed the acceptance
locally, then wrote the reply — and if the socket had dropped in between, the
reply never left. The matcher then correctly timed the offer out and re-offered
the trip to somebody else, and the simulator counted that correct retry as a
double dispatch.

The commit now happens only once the reply is on the wire. A lost reply is a
retry, not a duplicate. This was a flaw in the measurement, not in the system —
which is worth saying plainly, because it looked exactly like the failure the
whole sharding design exists to prevent.

### 2. Graceful shutdown hung the gateway

At 5,000 connections the gateway stopped answering while still running: 21.8%
CPU, 21 file descriptors, nothing listening on either port, and the process very
much alive.

`http.Server.Shutdown` waits for active handlers to return, and a WebSocket
handler returns only when its connection closes. So the gateway closed its
listener, waited for five thousand clients who had no reason to leave, and hung
— unreachable, and with the listener already gone so nothing reported it as
down. It now closes every connection first, then shuts the listener, then falls
back to `Close` if the grace period lapses.

### 3. Sequence numbers cannot survive a transport that reorders

The real one. Thirty per cent of all pings were being rejected as stale —
212,056 of them classified as *reordered*, which should be impossible for
records keyed by driver on a single partition.

A reconnecting client briefly holds two sockets. The gateway reads them with two
goroutines, and two pings for the same driver can reach the producer in the
wrong order. Judged on sequence alone, a perfectly fresh position arriving
behind a slightly older one is rejected — and once that starts, the matcher's
index goes stale and four out of five ride requests find nobody.

Ordering is now by the driver's own `SentAtMs`, with the sequence as tie-break.
The clock is stamped before the transport is involved, so it is immune to
whatever happens in between, and it increases with the sequence at the source.
The sequence still does the job no clock can: ordering the two halves of a cell
handover, which genuinely travel on different Kafka partitions.

That is the second time this project has been bitten by treating a counter as
more meaningful than it is. The first was a client restart resetting it to one;
this was a transport delivering it out of order. Both were invisible until the
load rig was pointed at them.

## Also worth recording

- **Trace sampling is not optional at this rate.** Exporting every span at
  10,000 pings/sec turned Jaeger into the bottleneck and filled the simulator's
  log with export timeouts — the same observer effect that broke the Phase 1
  benchmark, arriving by a different route. Head-based, parent-based sampling at
  1% keeps whole traces intact while making the collector a non-issue.
- **Redpanda's data volume did not survive an abrupt runtime shutdown**, exiting
  133 on a raft assertion. For a development rig the answer is to wipe the
  volume; it is worth knowing the failure looks like every service failing to
  dial at once.
- Memory at 10,000 connections: gateway 267 MB, simulator 170 MB. File
  descriptors 5,089 against a 61,440 limit, so sockets were never the ceiling.

## Reproducing

```bash
ulimit -n 200000
make up && make migrate
make dev-auth & make dev-ingest & make dev-matcher & make dev-trip & make dev-gateway &

SIM_TRANSPORT=ws SIM_DRIVERS=5000 SIM_REQUESTS_PER_SECOND=10 \
  OTEL_TRACES_SAMPLER_ARG=0.01 backend/bin/simd

curl -sX POST localhost:8101/sim/config -H 'content-type: application/json' -d '{"drivers":10000}'
curl -s localhost:9104/metrics | grep surge_gateway_
```

`SIM_TRANSPORT=kafka` restores the Phase 1 path, which is still the way to
isolate ingest from the gateway. `discard` measures the simulator alone.
