# surge

Ride-hailing, built to be a distributed systems project rather than a CRUD app
with a map on it. Amsterdam, one market, a geo-sharded matcher, and a driver
simulator that exists so every number here is measured rather than asserted.

**The backend is Go. The only Node in any request path is authentication, and
it is not in the path.** `apps/auth` runs better-auth and nothing else; the
browser trades its session cookie for a short-lived EdDSA token at
`/api/auth/token`, and every Go service verifies that signature locally against
`/api/auth/jwks`. No product endpoint touches Node or its session table.

```
apps/auth            better-auth, and only better-auth
apps/web             TanStack Start — console, rider, driver
apps/mobile          Expo and NativeWind — rider and driver
packages/domain      the contract both languages compile against
packages/client      platform-free Effect services over the Go API
packages/common      the atoms and helpers web and mobile share
packages/database    the connection and better-auth's schema
proto/               the API contract — gRPC, REST, OpenAPI, and the TS client
backend/             the Go module
  services/          one directory per microservice
  shared/            the only thing services may import
  tools/             the migrator and the scaffolder — not services
  migrations/        SQL the Go services own
deploy/              docker, compose, k8s, prometheus, grafana
docs/benchmarks/     what was measured, and what broke
```

A service is a directory you could lift into its own repository: `cmd/` is the
entrypoint, `internal/` is private to it — Go enforces that, so nothing can
reach into another service's state even by accident — and within it the layering
is `domain` (rules, no transport), `service` (logic), `infrastructure` (Kafka,
gRPC, Postgres, HTTP).

`backend/tools` and `backend/migrations` deliberately sit beside `services/`
rather than inside it. A migrator is a job and a scaffolder is a script; neither
is a thing that scales on anything.

    make scaffold NAME=pricing

Dependencies point one way, from `apps/` into `packages/`.

## The shape of the system

Six Go services earn their separation, and nothing else does.

|            | why it is separate                                                   | scales on   |
| ---------- | -------------------------------------------------------------------- | ----------- |
| `gateway`  | stateful WebSockets: registry, backpressure, slow-consumer eviction  | connections |
| `ingest`   | write-heavy; H3 assignment and cell transitions                      | ping rate   |
| `matcher`  | sharded, single-writer per geography                                 | geography   |
| `trip`     | transactional state machine, outbox, idempotency                     | not much    |
| `payments` | the only holder of processor credentials; Stripe must not stall trip | not much    |
| `chat`     | the only holder of the push credential; Expo must not stall trip     | messages    |
| `fleet`    | holds the identity key and the documents bucket; vets drivers        | drivers     |

**Chat's socket traffic is doorbells.** A message is stored with a gapless
per-conversation seq and announced on `ws.push` as `ChatChanged`, which names
the conversation and its newest seq. The client then reads what it has not
seen over REST, so a push that is lost or late costs nothing. A `ws.push` key
of `role:ops` reaches every connected ops user, which is how support's queue
stays live. The details are in `backend/services/chat/README.md`.

**A driver is approved by derivation, not by decree.** The fleet holds
vehicles and documents, and works out standing from them every time it is
asked: identity verified, one vehicle approved, every document valid today. A
licence or an inspection that lapses overnight withdraws the driver without
anybody deciding anything, and the result is published on `fleet.drivers`,
compacted and keyed by driver, for whoever dispatches. The matcher is that
consumer: with `MATCHER_REQUIRE_APPROVAL=true` every instance follows the whole
topic and offers no trip to a driver it does not find approved there. It is off
on a laptop, because the simulator's drivers never uploaded anything. Its integrations are
the RDW's open vehicle register, Stripe Identity on a restricted key, and a
model that reads insurance certificates for the reviewer. The two papers a
real Amsterdam driver needs — the VOG and the chauffeurskaart — have no API
anywhere, so they are modelled as states rather than pretended to be calls.
The details are in `backend/services/fleet/README.md`.

`core` holds everything boring — users, vehicles, pricing — as one service until
it hurts. It starts with profiles: the first name and photo a rider and a
driver see of each other, the photo re-encoded before it is stored in R2. The
details are in `backend/services/core/README.md`. `simd` is the load generator,
not a product service.

**One topic carries everything a matcher shard needs.** `geo.events` is a tagged
union keyed by H3 resolution-7 cell. One topic rather than four because no Kafka
consumer-group balancer guarantees co-partitioned assignment across topics — an
instance can hold partition 3 of one and partition 5 of another. With a single
topic, owning a partition means owning every event for those cells in total
order, which is what lets a shard be one goroutine over in-memory state with no
locks at all.

Two resolutions, and the difference is the architecture: **res 7 is the shard
cell and the Kafka key** (ownership), **res 9 is the driver index bucket**
(search). `geo.ShardCell` derives from `geo.IndexCell` rather than computing
independently, because H3 is not hierarchically consistent under `LatLngToCell`
and two answers would mean two matchers each believing they own one driver.

**Trip and payments never call each other.** Trip publishes `trip.lifecycle`
facts (`TripRequested`, `TripCompleted`, …) and payments answers on
`payment.events` (`PaymentAuthorized`, `PaymentFailed`, …), both keyed by trip
and both written through a transactional outbox (`shared/outbox`) in the same
transaction as the change they report. With `TRIP_REQUIRE_PAYMENT=true` a
booking waits in `payment_pending` and is sent to the matcher only once the
fare is held. Every processor call carries an idempotency key derived from the
trip, the attempt and the operation, and the ledger is double entry: a
transaction that does not sum to zero is a constraint violation. The details
are in `backend/services/payments/README.md`.

## The API client is generated, never written

`proto/trip.proto` is the only place the REST API is declared. `make proto`
turns it into the Go gRPC service, the grpc-gateway reverse proxy,
`docs/api/surge.swagger.json`, and — through `@effect/openapi-generator` —
`packages/domain/src/api/SurgeApi.ts`. Adding or changing an endpoint is an edit
to the proto and a `make proto`. Declaring endpoints by hand in TypeScript is
the drift this arrangement exists to prevent.

What the document cannot say for itself is said once, in the proto and in
`scripts/generate-api-client.mjs`:

- `openapiv2_operation` `tags` and `operation_id` name the client:
  `api.trips.create(...)`, not `api.TripService.TripServiceCreateTrip(...)`.
- `format` on a field names its TypeScript type — `trip-id` becomes a branded
  `TripId`, `cents` becomes `CentsFromString`, grpc-gateway's own `int64`
  becomes `Int64FromString`. The mapping is a table in the script; the schemas
  live in `packages/domain/src/api/Primitives.ts`. A new branded id is one
  `format` in the proto and one line in each.
- Every field is required, because the gateway marshals with `EmitUnpopulated`
  and an unassigned driver is `""`, not absent.
- Errors are `surge.common.v1.ErrorBody`, which the gateway's custom handler
  emits. grpc-gateway's default `rpcStatus` is disabled because nothing returns
  it.

`packages/domain/src/trip/Trip.ts` names the inlined shapes (`Trip`,
`FareQuote`, …) by deriving them from the generated types, never restating
them. `packages/domain/test/api/Generation.test.ts` decodes responses captured
from the running gateway (`pnpm capture:api-fixtures`) and fails if the pipeline
stops producing branded ids, decoded numbers or required fields — the way it
would break silently if the generator changed its output.

## Read the vendored Effect source before writing Effect code

`repos/effect` is the full Effect monorepo, vendored with `git subtree` at exactly the version
this repo depends on (`effect` in `pnpm-workspace.yaml`, currently `4.0.0-rc.109`). It also
covers `@effect/atom-react`, `@effect/vitest`, `@effect/sql-pg`, `@effect/platform-node`, and
the AI packages.

Read it **first**, not as a fallback. Do not wait until you feel unsure — Effect v4 is a
release candidate whose APIs moved recently, so confident recall is exactly the failure mode
this guards against. Open the real signature in `repos/effect/packages/*/src/` before you use
an API you have not already read in this session.

`repos/effect-form` is vendored the same way, at the versions installed here
(`@lucas-barake/effect-form@0.25.0-beta.6`, `@lucas-barake/effect-form-react@0.26.0-beta.5`).
Its published `latest` still targets Effect v3; the repo's `main` branch is the v4 line, which
is why the catalog pins the beta. There are no published docs for the v4 API, so
`repos/effect-form/packages/form*/src/` is the only reference — read it before using a form API.

## Precedence

When sources disagree, higher wins:

1. `repos/effect` — the actual source of the version installed here
2. `RULES.md` — hard repository rules
3. `knowledge/rules/` and `knowledge/skills/`
4. Your own recall — never authoritative for Effect APIs

This ordering applies to `RULES.md` and `knowledge/` themselves: where their example code
contradicts `repos/effect`, the vendored source is correct and the doc is stale. Their
_intent_ still stands — only the API spelling defers.

## Corrections already applied

`RULES.md` and `knowledge/skills/` have been corrected against rc.109. Each was confirmed by
reading the vendored source or by a compiler error, not inferred:

| Was                       | Now                                                                                                                           |
| ------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| `ServiceMap.*`            | `Context.*` — there is no `ServiceMap` module                                                                                 |
| `Schema.TaggedErrorClass` | `Schema.TaggedError`                                                                                                          |
| `.asEffect()`             | removed in v4; yieldables that are Effects pipe directly, and `Option`/`Result` use `Effect.fromOption` / `Effect.fromResult` |
| `Effect.fromYieldable`    | does not exist                                                                                                                |
| schema `makeUnsafe()`     | `.make()`, which validates                                                                                                    |
| `@effect/platform`        | gone in v4 — `effect/unstable/http`, `.../socket`, or `@effect/platform-node` / `-browser`                                    |

`Latch.makeUnsafe`, `Ref.makeUnsafe`, and `Deferred.makeUnsafe` are **real** and were left alone —
`makeUnsafe` is only wrong on schemas.

`knowledge/skills/` diverges from the upstream dotfiles repo on purpose. `pnpm sync:skills`
re-applies every correction above after fetching, and fails without writing if any known drift
survives — so a sync cannot silently reintroduce v4-invalid APIs.

Assume more drift exists than is listed here. Check the source.

## Do not touch `repos/`

- Never edit files under `repos/` unless explicitly asked.
- Never import from `repos/`. Application code imports from normal package dependencies.
- `no-relative-import-outside-package` and the `@/` alias are for intra-repo imports only.

Update a vendored copy when its pinned version moves:

```
git subtree pull --prefix=repos/effect https://github.com/Effect-TS/effect.git main --squash
git subtree pull --prefix=repos/effect-form https://github.com/lucas-barake/effect-form.git main --squash
```

## Go

One module, `backend/go.mod`, module path `github.com/ishakdeveloper/surge`. It
lives under `backend/` rather than at the root so `pnpm -r` and `tsc -b` never
see Go files.

- `pkg/` is the only cross-service importable surface. `internal/<service>/` is
  that service's own.
- Wiring is manual constructor injection in `cmd/*/main.go`. No DI framework.
- `pkg/config` draws one line and it matters: an **unset** variable may take a
  default, a **set but unparseable** one is always an error. Silent fallback on
  parse failure is how a service ends up listening on a port nobody chose.
- Errors are wrapped with `%w` and context. Typed errors where the caller can
  act — `routing.NoRouteError` is an outcome, not a failure, because the
  simulator drops points into water on purpose.
- `gofmt` is the whole style guide. CI fails on a diff.

## Commands

Everything routine is a make target; `make help` lists them.

|                                       |                                        |
| ------------------------------------- | -------------------------------------- |
| `make up` / `make down`               | infrastructure in Docker               |
| `make up-core`                        | without the dashboards                 |
| `make bench-matching RPS=20`          | greedy vs batched matching, A/B        |
| `make check-handover`                 | a clean matcher stop leaves no lag     |
| `make migrate`                        | apply database migrations              |
| `make dev-auth`                       | the auth service                       |
| `make dev-mobile`                     | the Expo app, on a simulator or phone  |
| `make dev-ingest` / `make dev-sim`    | the Go services, on the host           |
| `make dev-trip` / `make dev-payments` | trip, and payments beside it           |
| `make dev-chat`                       | chat, with notifications faked         |
| `make dev-fleet`                      | driver vetting, against the real RDW   |
| `make stripe-listen`                  | Stripe's webhooks to the gateway       |
| `make bench-payments RPS=5`           | what holding the fare adds to dispatch |
| `make load DRIVERS=40000`             | turn the knob                          |
| `make control`                        | simulator-only run, pings discarded    |
| `make stats`                          | current simulator and ingest state     |
| `make test`                           | both suites                            |
| `make check`                          | `tsc -b`, oxlint, dprint               |

Go services run on the **host**, not in Docker: the reload loop is a compile
rather than an image build, and the 8 GB Docker VM is left to the things that
need it.

`pnpm check`'s second half is `tsconfig.tools.json`, which type-checks what
project references cannot — the Vite and Vitest configs, `vitest.shared.ts`,
`setupTests.ts`, `.railway/railway.ts`.

## Ports

Deliberately off the defaults. This machine already runs a Postgres on 5432, a
Redis on 6379, and other projects on 3000, 3001, 3100 and 5173 — an ambiguous
bind is a debugging trap you only notice an hour later. The web app is on 5273
rather than Vite's own 5173 for exactly that reason: another project's dev
server had it, and auth and the gateway each trust a single web origin.

|                  |       |          |       |
| ---------------- | ----- | -------- | ----- |
| auth             | 3200  | Postgres | 55433 |
| Redpanda         | 19092 | Redis    | 56380 |
| Redpanda Console | 8080  | Valhalla | 8002  |
| Prometheus       | 9090  | Grafana  | 3005  |
| Jaeger           | 16686 | web      | 5273  |

Go services take 8100+ for their APIs and 9101+ for metrics: `simd` 8101/9101
(and 8111 for its gRPC control, which the gateway serves as `/v1/simulator`),
`ingest` 8102/9102, `trip` gRPC on 8110 and metrics on 9105, `payments` gRPC on
8112 and metrics on 9107, `chat` gRPC on 8113 and metrics on 9108, `core`
gRPC on 8114 and metrics on 9109, and `fleet` gRPC on 8115 and metrics on
9110.

`CHAT_PUSH_PROVIDER` has no default and takes `expo` or `fake`, for the same
reason: a deploy that forgets to choose fails at boot rather than running
while nobody's phone ever buzzes. `FLEET_IDENTITY_PROVIDER` and
`FLEET_DOCUMENT_STORE` have no defaults for the same reason again — the one
that approves drivers nobody checked is the worst of the three.

`PAYMENTS_PROCESSOR` defaults to `stripe` and needs `STRIPE_SECRET_KEY` and
`STRIPE_WEBHOOK_SECRET`, so a deploy that forgets to choose fails for want of a
key rather than looking healthy while every ride is free. `fake` simulates
Stripe's test cards and charges nobody; it is what benchmarks run on.

## Testing

The repo rule is 80% coverage, and the tests that matter here are the ones that
cross a boundary no compiler checks:

- `backend/shared/authz` signs a user up against a **running auth service** and
  verifies the token. It is the only thing that can catch the claim names,
  algorithm, issuer, audience and role clamp disagreeing across languages.
- `backend/shared/geo` decodes a **committed Valhalla fixture** rather than a
  hand-made polyline, because precision-6 shape decoded at precision 5 is a
  well-formed route that is wrong by a factor of ten.
- `packages/client/test/platform-free.test.ts` enforces that nothing shared
  imports a platform package, so `apps/mobile` stays cheap.
- `services/payments/internal/infrastructure/stripe` runs the adapter against
  **Stripe's test mode**, and refuses anything but an `sk_test_` or `rk_test_`
  key. The fake processor is what every other test uses, and this is the only
  thing that can catch the two disagreeing.
- `apps/auth/test/sms/TwilioContract.test.ts` sends through **Twilio with its
  test credentials** — `TWILIO_TEST_*`, never the live ones beside them — and
  holds the fake HTTP client in `Sms.test.ts` to the error codes Twilio really
  answers its magic numbers with.

Tests needing Redpanda, Valhalla, Postgres, Stripe or the auth service **skip**
when it is absent rather than failing, so `go test ./...` and `pnpm test` stay
useful on a bare machine. Signing in is a one-time code, so the tests that sign
up against a running auth service read it from its dev outbox, and skip unless
it runs with `AUTH_DEV_OUTBOX=true` — never set in a deployment. The Go Postgres tests go through `shared/pgtest`,
which reads `DATABASE_URL`, falls back to the dev database on 55433, and gives
each test its own migrated schema. CI runs them against a Postgres service
container, and runs the Stripe contract test when the repository has a
`STRIPE_TEST_SECRET_KEY` secret.

Use a fresh `INGEST_GROUP` for each benchmark run, or you measure the previous
run's backlog. That mistake is documented in `docs/benchmarks` rather than
fixed, because the fix was a design decision.

## Migrations

Applied by a script, never at boot — two instances starting together would both
migrate. Every migration is idempotent and there is no ledger, so applying the
whole set to any database converges it on the committed schema.
`packages/database/test/Migrations.test.ts` holds that honest by applying them
twice.

`0001_auth.sql` is **generated** by `compileAuthMigrations` from the plugin set
in `apps/auth/src/iam/Options.ts`, then committed. Change the plugins, regenerate
it; do not hand-edit.

## Full rules

`RULES.md` holds the hard repository rules — Effect style, architecture, forms,
notifications, observability, testing, commits. Read it before making changes.
`knowledge/README.md` indexes the per-topic guides.
