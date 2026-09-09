# Surge

Ride-hailing for one city, built the way the interesting parts demand: a
geo-sharded matcher where the race condition cannot exist, a stateful WebSocket
gateway, a trip state machine that survives process death mid-transition — and,
before any of it, a driver simulator that puts tens of thousands of cars on real
Amsterdam roads so that every number is measured rather than claimed.

The simulator came first on purpose. Without a load generator you build the
whole system, test it with three browser tabs, and never meet a single
interesting failure mode. With one you get a knob to turn until things break.

## Where it is

**Phase 1 is done and measured.** 10,000 GPS writes/sec sustained at 40,000
simulated drivers, p99 end-to-end age of 50 ms, no dropped or out-of-order
pings. It holds to 15,000/sec before anything moves.

Three things broke on the way, and only the third was where it looked — the
first was the telemetry measuring the consumer, the second was replaying six
hours of stale positions on startup, and the third was the load generator
itself. [`docs/benchmarks`](docs/benchmarks) has the numbers and the reasoning.

Next is the matcher: consumer-group partitions as geographic shards, single
writer per shard, cross-shard reservation, and what happens to in-flight offers
when the group rebalances.

## Running it

```bash
cp .env.example .env      # then set AUTH_SECRET and SIM_TOKEN_SECRET
make up                   # infrastructure; Valhalla builds tiles once, 10-20 min
make migrate
make dev-auth             # in its own terminal
make dev-ingest           # and its own
make dev-sim              # and its own

make load DRIVERS=40000   # turn the knob
make stats
```

Grafana is on <http://localhost:3005> with the ingest dashboard provisioned;
Redpanda Console on <http://localhost:8080>.

Infrastructure runs in Docker, Go services on the host. That is not laziness:
the reload loop becomes a compile rather than an image build, and the 8 GB
Docker VM is left for Redpanda and Valhalla.

## How it works

**Two H3 resolutions carry the architecture.** Resolution 7 (~5 km²) is the
shard cell — it is the Kafka message key, so it is also the unit of ownership.
Resolution 9 (~0.1 km²) is the index bucket a driver's position lands in.
Amsterdam covers about 130 shard cells and 2,100 index cells.

**Matching under contention needs no lock.** Two riders, one nearby driver is
the classic race, and the usual answer is a distributed lock. Here it cannot
happen: one matcher instance owns a Kafka partition, a partition owns a set of
shard cells, and that instance is the only writer for every driver standing in
them. Searches may cross shards; reservations are always routed to the shard
that owns the driver, where they are processed serially.

**Everything a shard needs is one topic.** `geo.events` carries a tagged union
keyed by shard cell — driver movement, ride requests, reservations, replies. One
topic rather than four, because no consumer-group balancer guarantees
co-partitioned assignment across topics. With one, owning a partition means
owning every event for those cells in total order, and a shard collapses to a
single goroutine over in-memory state.

**Authentication is the only Node in the system, and it is off the hot path.**
`apps/auth` runs better-auth; the browser exchanges its session cookie for a
short-lived EdDSA token, and Go verifies the signature locally against the
published JWKS. No product request reaches Node or its database.

## Stack

Go 1.25 · Redpanda · Postgres · Redis · Valhalla · H3 · Prometheus + Grafana ·
Effect v4 · TanStack Start · better-auth

Built on the [forge-effect](https://github.com/ishakdeveloper/forge-effect)
boilerplate, with its server half replaced by Go.
