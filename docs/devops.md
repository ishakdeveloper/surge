# DevOps — from a laptop to GKE

Everything in `docs/benchmarks` was measured on one machine. The system runs,
the numbers are real, and none of it has left the laptop. This is the plan for
getting it off: CI that tests what it claims to, Playwright against the whole
stack, images that are built once and promoted, and a GKE deployment in
`europe-west4` — Eemshaven, the region closest to the one market we serve.

Each phase ships on its own. None of them needs the next one to be worth doing.

## Where we are

**CI** is one workflow, `.github/workflows/ci.yml`, with two jobs: `check`
(dprint, `tsc -b`, oxlint, `pnpm test`) and `go` (gofmt, `go vet`, `go test`).
What it does not do is the more interesting list:

- **Coverage is not enforced.** It runs `pnpm test`, not `pnpm coverage`, so
  the 80% floor in `RULES.md` is a rule nobody checks. Go has no gate at all.
- **Generated code is not checked for drift.** `make proto`,
  `make wire-fixtures`, `0001_auth.sql` and `routeTree.gen.ts` are all
  committed output that can silently fall behind their source.
- **The integration tests always skip.** Nothing starts Redpanda, Valhalla or
  auth, so `packages/client/test/*.integration.test.ts` and the Go tests in
  `shared/kafkax`, `shared/routing` and `shared/authz` report as passing
  without running. Only the testcontainers Postgres under `apps/auth` is real.
- **No image is built.** `deploy/k8s/production/kustomization.yaml` says "CI
  replaces the tag with the commit sha". CI does not.

**Kubernetes manifests** exist: Kustomize, `base` plus `development` and
`production` overlays, driven locally by `make k8s-up` and the Tiltfile on
OrbStack. The production overlay has never been applied anywhere, and it shows.

**`.railway/railway.ts`** came with the boilerplate the repo started from. It
deploys auth and web only — no Go, no Kafka — and wires `DATABASE_URL` where
auth reads `AUTH_DATABASE_URL`. It is not a deployment target; it should go.

## Phase 0 — what is already wrong

Found while reading for this document. None of it needs a cloud account to fix,
and all of it would bite on the first real deploy.

| Defect                                                                                                                                                                                                                           | Where                                                     |
| -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------- |
| The production `migrate` Job never gets `DATABASE_URL`. It reads only the ConfigMap, the value lives only in the Secret, and `patch-managed-services.yaml` targets the four services but not the Job.                            | `deploy/k8s/base/migrate-job.yaml`                        |
| Production auth still has `AUTH_BASE_URL=http://auth:3200` and `WEB_URL=http://localhost:5273`, and `AUTH_ISSUER` is the in-cluster URL. Cookies, CORS and the token issuer would all be wrong.                                  | `deploy/k8s/base/auth.yaml`, `deploy/k8s/base/config.env` |
| `EnsureTopics` hard-codes replication factor 1. A three-broker cluster will accept it and lose data on the first broker loss. Needs `KAFKA_REPLICATION_FACTOR` through `shared/config`, and SASL/TLS options for a real cluster. | `backend/shared/kafkax/topics.go`                         |
| The production `imagePullPolicy` patch replaces `IfNotPresent` with `IfNotPresent`, and every image is `:latest`.                                                                                                                | `deploy/k8s/production/kustomization.yaml`                |
| Web has no manifest, and `make images` does not build it.                                                                                                                                                                        | `deploy/k8s/base/`, `Makefile`                            |
| The web image bakes `VITE_AUTH_BASE_URL` in at build time and has no build argument at all for the API and WebSocket URLs, so one image cannot serve two environments.                                                           | `apps/web/Dockerfile`                                     |
| The gateway reaches the simulator on `SIM_GRPC_ADDR`, which nothing in k8s sets, and the simulator Service does not expose 8111. `/v1/simulator` cannot work in-cluster.                                                         | `deploy/k8s/development/simulator.yaml`                   |
| The gateway's `/ready` returns `ready` unconditionally, and no Go service has a readiness or startup probe.                                                                                                                      | `backend/services/gateway/cmd/main.go`                    |
| Prometheus scrapes `core` on 9106. There is no `core` service. Comments point at `11-secrets.yaml` and `21-migrate-job.yaml`, which no longer exist.                                                                             | `deploy/prometheus/prometheus.yml`, `deploy/k8s/`         |
| `make proto` installs every protoc plugin `@latest`. Two machines a week apart generate different code, which makes a drift check impossible.                                                                                    | `Makefile`                                                |
| Every workload reads one `surge-config` ConfigMap. Kustomize suffixes it with a content hash, so changing any one service's setting renames it and rolls all of them.                                                            | `deploy/k8s/base/kustomization.yaml`                      |
| `.railway/railway.ts` and the `railway` devDependency.                                                                                                                                                                           | delete                                                    |

The readiness point deserves a sentence, because the manifests left it off on
purpose — "so a Kafka blip does not become an outage". That reasoning is right
for **liveness**, which should stay dumb. Readiness is a different question:
not "is this process wedged" but "should the load balancer send it traffic". A
gateway that has not loaded the JWKS, or cannot reach trip, should not be in
rotation. That check should cover only this pod's own prerequisites, never a
shared dependency whose failure would drain every replica at once.

## One change, one rollout

A commit that touches the matcher should restart the matcher and nothing else.
That is not something the deploy has to be clever about. Kubernetes already
does it: `kubectl apply` over every Deployment rolls only the ones whose
pod template changed. Applying everything is fine. What breaks it is giving an
unchanged service a new image reference, and three things in the plan would do
exactly that:

- **Tagging every image with the commit SHA.** Every commit changes every tag,
  and every service rolls. Images are addressed by **digest** instead. A
  service whose inputs did not change builds to the same bytes, gets the same
  digest, and its Deployment is left alone.
- **One shared ConfigMap.** See Phase 0: each service gets its own, generated
  from its own `config.env`, and the few genuinely shared values (`AUTH_*`,
  `KAFKA_BROKERS`) are the only thing in a shared one.
- **`backend/shared/`.** A change there should redeploy the services that import
  the changed package, and only those. `go list -deps ./services/<svc>/cmd`
  lists each service's packages exactly. On the TypeScript side
  `pnpm --filter "...[origin/main]"` does the same for auth and web.

Same digest, no rollout, only works if builds are **reproducible**. Most of that
is already true: the Go Dockerfiles build with `-trimpath`, and `.dockerignore`
excludes `.git`, so Go cannot stamp a commit into the binary. What is left is
layer timestamps (`SOURCE_DATE_EPOCH` and buildx `rewrite-timestamp`) and the
unpinned `apk add` in the runtime stage, which drifts when Alpine publishes.

The two mechanisms back each other up. Dependency detection decides what CI
bothers to build and test. The digest decides what actually rolls. If detection
misses a dependency, the rebuilt image comes out different and rolls anyway. If
detection is too cautious, the rebuild is a cache hit with an identical digest
and nothing happens.

### How the large shops do it

The tooling differs and the principles do not:

- **Netflix** keeps mostly one repository per service, each with its own
  Spinnaker pipeline — Spinnaker is theirs. Artifacts are immutable (machine
  images historically, containers on Titus now). Deploys are red/black. A canary
  takes a slice of traffic, Kayenta compares its metrics against the baseline
  and rolls back on its own, and the rollout moves one region at a time.
- **Google** is one monorepo. Blaze, open-sourced as Bazel, knows the exact
  dependency graph, so a commit builds and tests only the targets it affects.
  Releases go out progressively, cluster by cluster.
- **Uber** is a Go monorepo on Bazel, with SubmitQueue — a merge queue — keeping
  `main` green at their commit rate. Services still deploy on their own
  schedules.

What they share: **services deploy independently**, even from a monorepo. An
artifact is **built once and promoted**, never rebuilt per environment. Only
**what changed** is built and tested, which in a monorepo means a dependency
graph. Rollouts are **progressive** — canary first, promote or roll back on
metrics. **Feature flags** separate shipping code from turning it on. And
changes are **compatible across versions**, which is the one that bites.

### Compatibility is the price of independent deploys

Once services roll separately, an old gateway runs next to a new trip, and a new
ingest writes `geo.events` that an old matcher reads — during every rollout, and
for as long as a rollout is paused. The wire fixtures catch Go and TypeScript
disagreeing about the same version. They do not catch one version meeting the
next.

So a change to a contract lands in two steps: **expand**, then **contract**. Add
the field, deploy the reader everywhere, then start writing and depending on it.
Remove a field only once nothing reads it. For protobuf, CI enforces this with
`buf breaking` against `main`. `buf` is used only as a checker here; `make proto`
keeps generating with protoc. For `geo.events`, the rule is that variants and
fields are only ever added, and a golden fixture from `main` has to decode on
the branch.

## Phase 1 — CI that tests what it claims to

**One workflow, path-filtered, with one required check.** A `changes` job runs
`dorny/paths-filter` and every other job is conditional on it. A final `ci-ok`
job depends on all of them and is the only status check branch protection
requires. Separate workflows with `paths:` look simpler and are not: a required
check from a workflow that never triggered stays pending forever.

| Job           | Runs when            | What                                                                                                                                  |
| ------------- | -------------------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| `ts`          | TS, proto            | dprint, `pnpm check`, oxlint, **`pnpm coverage`**                                                                                     |
| `go`          | Go, proto            | gofmt, `go vet`, `go test -race -coverprofile`, an 80% gate on non-generated packages, `staticcheck`, `govulncheck`                   |
| `codegen`     | proto, Go, TS        | `make proto`, `make wire-fixtures`, auth migration and route tree regeneration, then `git diff --exit-code`                           |
| `manifests`   | `deploy/`            | `kubectl kustomize` every overlay, piped through `kubeconform`                                                                        |
| `compat`      | proto, `shared/wire` | `buf breaking` against `main`; the golden `geo.events` fixtures from `main` decode on the branch                                      |
| `affected`    | always               | `go list -deps` per service and `pnpm --filter "...[origin/main]"`; outputs the services the other jobs build and test                |
| `images`      | affected services    | build only those images, reproducibly; push on `main` only                                                                            |
| `integration` | Go, TS               | Redpanda and Postgres as service containers, `KAFKA_BROKERS` and `TEST_DB_URL` set, so the tests that skip on a bare machine run here |
| `e2e`         | web, backend, proto  | Phase 3                                                                                                                               |

`gofmt` stays the style guide. `staticcheck` and `govulncheck` are there for
correctness and known CVEs, not taste — which is why this is not
golangci-lint with forty linters switched on.

**The drift check needs reproducible codegen first.** Go 1.24 added `tool`
directives to `go.mod`, so the four protoc plugins become
`go tool protoc-gen-go` at a version `go.sum` pins, instead of whatever was
`@latest` that day. protoc itself gets pinned in CI, and Go and protoc go into
`flake.nix` next to Node and pnpm, so there is one toolchain definition for the
laptop and the runner.

**Images.** Eight: gateway, ingest, matcher, trip, simulator, migrate, auth,
web. Buildx with the GitHub Actions cache, a Trivy scan, amd64 only. h3-go is
cgo, and the Dockerfiles already explain why that means Alpine rather than
distroless; it also means cross-building arm64 under QEMU is slow enough to
avoid. If Axion nodes are ever worth it, `ubuntu-24.04-arm` runners build them
natively.

**Hygiene.**

- `permissions: contents: read` at the top of every workflow, widened per job.
- Third-party actions pinned by commit SHA.
- **Renovate, not Dependabot**, because it understands the pnpm catalog, Go
  modules, Dockerfiles and actions in one config. `effect` and `@effect/*` are
  excluded from automatic bumps: each one is paired with a `git subtree pull`
  of `repos/effect`, and an rc bump without it is exactly the drift `AGENTS.md`
  warns about.
- lefthook for gofmt and dprint on staged files — optional, since CI is the
  real gate.

## Phase 2 — images, built once and promoted

**Artifact Registry in `europe-west4`**, pushed to through **Workload Identity
Federation**: GitHub's OIDC token is exchanged for a short-lived GCP credential,
so there is no service-account JSON key in a GitHub secret. This needs a small
first slice of Terraform — the registry, the WIF pool and a service account —
which is why it comes before the rest of the cloud.

- Images are pushed with the **git SHA** as a tag, for people to read, and
  **deployed by digest**, which is what decides whether anything rolls — see
  _One change, one rollout_. `latest` is never deployed.
- `docker/build-push-action` emits SBOM and provenance attestations. cosign and
  Binary Authorization can come later; attestations cost nothing now.
- **Web gets its URLs at runtime.** The Nitro server serves them to the client
  from its own environment instead of Vite inlining them at build time. Without
  this, staging and production need two web builds, and the image that was
  tested is not the image that ships.

## Phase 3 — Playwright against the whole stack

The tests worth having here are the ones that cross every boundary at once: a
browser, better-auth, the token exchange, the gateway, Kafka, the matcher and
trip, and back out over a WebSocket. Mocking the gateway would skip exactly the
part this project is about.

**Where it lives: `e2e/` at the root**, as `@surge/e2e` in
`pnpm-workspace.yaml` and the tsconfig references. Not `apps/e2e` —
`vitest.config.ts` has `projects: ["apps/*"]`, and Vitest adopts a matching
folder even without a config, then runs its `*.spec.ts` files as Vitest tests.

**The stack is the images CI just built.** A compose `app` profile (or overlay
file) runs auth, web and the Go services from the `images` job's output next to
the infrastructure compose already has, with both migrators as one-shot
containers first. E2E then tests the artifacts that ship rather than a dev
server, and locally the same thing is `make e2e`.

**Valhalla gets a pre-baked image.** Building Noord-Holland tiles at container
start takes minutes, every run. `surge-valhalla-ams` bakes them in, is rebuilt
weekly, and lives in the registry. The same image serves Phase 4.

**The tests:**

- sign up, sign in, sign out
- a rider and a driver in **two browser contexts**: the rider requests, the
  driver accepts, the trip runs to completion, and both screens agree at every
  step
- the ops console, with the role granted in global setup: the fleet map shows
  simulator drivers, with `SIM_ENABLED` and a fleet of tens, not thousands

**Rules carried over from `RULES.md`.** Playwright's auto-waiting locators and
`expect.poll`, never a sleep. A unique user per test. A fresh consumer group per
run, which is the `INGEST_GROUP` lesson from `docs/benchmarks` again. On
failure: the trace, the video, and every service's logs as artifacts.

**The same job un-skips what already exists.** With `SURGE_API_URL` and
`AUTH_BASE_URL` pointing at the stack, the `client-integration` Vitest project
and `backend/shared/authz` run for real, and `pnpm capture:api-fixtures` can be
diffed against the committed fixtures instead of being rerun by hand.

Runs on PRs that touch web, backend or proto, and nightly on `main`.

## Phase 4 — GKE

**GKE Standard, regional, `europe-west4`**, with staging and production as
separate GCP projects. Standard over Autopilot because two things here need
control over nodes: Redpanda wants its own pool and local SSD, and the gateway's
connection density wants raised ulimits and sysctls.

| Locally          | On GKE                                                                                       |
| ---------------- | -------------------------------------------------------------------------------------------- |
| Postgres         | Cloud SQL for Postgres 17, private IP, IAM auth; `surge` and `surge_auth`                    |
| Redpanda         | Redpanda's Helm chart in-cluster: three brokers, RF 3, a dedicated node pool                 |
| Redis            | Memorystore                                                                                  |
| Valhalla         | a Deployment of `surge-valhalla-ams` — read-only tiles, stateless, scales like anything else |
| Jaeger           | OTLP to Cloud Trace                                                                          |
| Prometheus       | Google Managed Prometheus, `PodMonitoring` over the existing `/metrics`; dashboards kept     |
| `.env`           | Secret Manager, synced by External Secrets Operator                                          |
| `localhost:8100` | Gateway API on a global external ALB, Certificate Manager, Cloud DNS                         |

**Why Redpanda in the cluster** rather than Managed Service for Apache Kafka:
the matcher's design and every number in `docs/benchmarks` were measured
against Redpanda. Running the same broker in production keeps those numbers
meaningful. Managed Kafka is the no-ops alternative if running brokers stops
being worth it; franz-go speaks both.

**Manifests stay Kustomize.** A `gke` component adds what is GCP-specific: the
Gateway and HTTPRoutes, a `GCPBackendPolicy` for WebSocket timeouts,
`PodMonitoring`, `ExternalSecret`s, and Workload Identity annotations on
ServiceAccounts. Third-party software — Redpanda, External Secrets — comes from
its own Helm charts. Our manifests do not become a chart; nobody else installs
them, and templating would only hide what is applied.

**Workloads:**

- readiness and startup probes, separate from a liveness probe that stays dumb
- requests set from the benchmarks, not guessed
- `topologySpreadConstraints` across zones; the PDBs and HPAs that exist
- NetworkPolicies: only the gateway takes ingress; only trip talks to Postgres
- the gateway gets a `preStop` delay so the ALB stops routing to it before it
  closes sockets — and clients must reconnect regardless, because the ALB puts
  a ceiling on how long a WebSocket lives
- the matcher rolls with `maxUnavailable: 1` and never runs more replicas than
  `geo.events` has partitions; a replica past that owns nothing

## Phase 5 — infrastructure as code, and delivery

**OpenTofu in `deploy/terraform/gcp/`**, state in a GCS bucket. Modules for
project APIs, the VPC, GKE, Artifact Registry, Cloud SQL, Memorystore, the WIF
pool, DNS, and Secret Manager secrets as **containers only** — values are set
out of band and never pass through state. `tofu plan` runs on PRs that touch it;
`apply` waits for approval.

**Delivery starts push-based**, from `deploy.yml`:

1. authenticate with WIF
2. `kustomize edit set image surge/<svc>=…@sha256:<digest>` for every service,
   using the digest just built for affected ones and the digest already
   deployed for the rest
3. apply the two migrate Jobs and wait for them to complete
4. `kubectl apply -k`, then `kubectl rollout status` — which only has something
   to wait for on the services whose digest moved

Promotion is GitHub Environments: staging deploys from `main` automatically,
production needs an approval. Migrations are idempotent with no ledger, so
running them on every deploy is safe, and it keeps the rule that they are
applied by a job and never at boot. Argo CD earns its place when there is more
than one cluster; one cluster and one workflow do not need a reconciler.

**Canaries come later, and the gateway comes first.** Argo Rollouts does in the
cluster roughly what Spinnaker and Kayenta do for Netflix: move a fraction of
traffic, compare error rate and latency against the stable version, then promote
or abort. The gateway is where a bad rollout hurts most, because it drops live
WebSockets rather than failing single requests. The matcher is the poor
candidate: a partition has one owner, so "10% of traffic" is not something it
can do.

**Measured in the cloud too.** A `workflow_dispatch` benchmark runs `simd` in a
`loadtest` namespace against staging, on a spot node pool, and writes its
results into `docs/benchmarks/`. The project's rule is that numbers are
measured, not asserted; that should not stop being true once the system is off
the laptop.

**Cost.** Budget alerts per project, staging scaled to zero overnight, spot
nodes for load tests.

## Order

- [ ] **P0** fix the defects above, including per-service ConfigMaps; delete
      Railway
- [ ] **P1** the CI split, coverage gates, pinned codegen and the drift check,
      manifest validation, `buf breaking` and `geo.events` compatibility,
      affected-service detection, Renovate
- [ ] **P2** Terraform bootstrap (registry, WIF), reproducible images deployed
      by digest, runtime config for web
- [ ] **P3** `surge-valhalla-ams`, the compose `app` profile, Playwright
- [ ] **P4** GKE, Cloud SQL, Redpanda and the `gke` component; staging deploys
      from `main`
- [ ] **P5** production behind approval, Managed Prometheus and Cloud Trace,
      cloud benchmarks; Argo Rollouts canaries for the gateway
