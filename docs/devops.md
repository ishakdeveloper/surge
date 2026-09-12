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
deployed auth and web only — no Go, no Kafka — and wired `DATABASE_URL` where
auth reads `AUTH_DATABASE_URL`. It was not a deployment target, and Phase 0
deleted it.

## Phase 0 — what was already wrong

Found while reading for this document, and fixed first: none of it needed a
cloud account, and all of it would have bitten on the first real deploy. Both
overlays now render and pass `kubeconform`, and every image builds. Rows marked
_new_ were found while fixing the rest.

| Defect                                                                                                                                                                                                                                     | Fix                                                                                                                                                                                                                                                                                                              |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| The production `migrate` Job never got `DATABASE_URL`: it read only the ConfigMap, the value lived only in the Secret, and the credential patch targeted four Deployments but not the Job. Auth and its migration never got theirs either. | A `surge-database` Secret, referenced by key and only by what uses it: trip, payments and `migrate` get `DATABASE_URL`; auth and `migrate-auth` get `AUTH_DATABASE_URL`. Development generates it, production supplies it.                                                                                       |
| Production auth had `AUTH_BASE_URL=http://auth:3200` and `WEB_URL=http://localhost:5273`, and `AUTH_ISSUER` was the in-cluster URL. Cookies, CORS and the token issuer would all have been wrong.                                          | Public addresses live in the overlays. The issuer is the public address auth signs as; the JWKS is still fetched in-cluster.                                                                                                                                                                                     |
| `EnsureTopics` hard-coded replication factor 1, which a three-broker cluster accepts and then loses data with.                                                                                                                             | `kafkax.Cluster`, read once from `KAFKA_BROKERS`, `KAFKA_REPLICATION_FACTOR`, `KAFKA_TLS` and `KAFKA_SASL_*`, and carried by every client in every service. Production asks for three.                                                                                                                           |
| The `imagePullPolicy` patch replaced `IfNotPresent` with `IfNotPresent`, and every image was `:latest`.                                                                                                                                    | Gone. Production names registry images with no tag; the deploy pins each to a digest.                                                                                                                                                                                                                            |
| Web had no manifest, and `make images` did not build it.                                                                                                                                                                                   | `base/web.yaml`; `make images` and Tilt build it.                                                                                                                                                                                                                                                                |
| _New._ Payments had no Dockerfile, no manifest and no Tilt resource, and the gateway's `PAYMENTS_GRPC_ADDR` was unset in the cluster.                                                                                                      | All four, and a PDB. Production sets `TRIP_REQUIRE_PAYMENT=true`.                                                                                                                                                                                                                                                |
| The web image baked `VITE_AUTH_BASE_URL` in at build time and had no way to set the API and WebSocket URLs, so one image could not serve two environments.                                                                                 | The server reads its environment and hands the browser the answer in the document, `apps/web/src/lib/public-config.ts`. Server-side session lookups use `AUTH_INTERNAL_URL` and never leave the cluster. The image has no build arguments.                                                                       |
| The gateway reached the simulator on `SIM_GRPC_ADDR`, which nothing set, and the simulator Service did not expose 8111.                                                                                                                    | Set in development, and exposed.                                                                                                                                                                                                                                                                                 |
| The gateway's `/ready` returned `ready` unconditionally, and no Go service had a readiness or startup probe.                                                                                                                               | `/ready` answers 503 until every subsystem has started and again from the moment shutdown begins, behind a `preStop` sleep. Trip and payments report `NOT_SERVING` before `GracefulStop`, which Kubernetes probes over gRPC natively. Every Go service has a startup probe, so liveness never races a slow boot. |
| _New._ The gateway's gRPC clients used `pick_first` over a ClusterIP: one HTTP/2 connection to one trip pod, held for the life of the process, however many replicas there were.                                                           | Headless Services, `round_robin`, and a five-minute `MaxConnectionAge` on the servers so a scale-up is found.                                                                                                                                                                                                    |
| Comments pointed at `11-secrets.yaml` and `21-migrate-job.yaml`, which no longer exist. (The first draft also listed a Prometheus scrape of a `core` service on 9106; there is none on `main`.)                                            | Fixed, `.gitignore` included.                                                                                                                                                                                                                                                                                    |
| The first draft said `make proto` installs its plugins `@latest`. It does not — each is `go install`ed at a version — but nothing checks that version against `go.mod` either.                                                             | Left for Phase 1, which moves them to `tool` directives pinned by `go.sum`.                                                                                                                                                                                                                                      |
| Every workload read one `surge-config` ConfigMap, so changing any one service's setting rolled all of them.                                                                                                                                | One ConfigMap per workload, plus `surge-shared` for what every Go service reads: the brokers, the replication factor, tracing.                                                                                                                                                                                   |
| _New._ Redis ran in compose and in the development cluster, and `REDIS_ADDR` was set, but nothing read it.                                                                                                                                 | Removed.                                                                                                                                                                                                                                                                                                         |
| _New._ Six Go Dockerfiles, identical but for the package path.                                                                                                                                                                             | One, `deploy/docker/go.Dockerfile`, with the package as a build argument.                                                                                                                                                                                                                                        |
| _New._ Neither the auth nor the web image built: `pnpm install` applies `patchedDependencies`, and the install stage never copied `patches/`.                                                                                              | Copied before the install.                                                                                                                                                                                                                                                                                       |
| _New._ Valhalla's compose health check had never passed: it called `wget`, which the image does not have. Nothing waited on it until the end-to-end stack did, and then nothing behind it started.                                         | `curl` against `127.0.0.1`.                                                                                                                                                                                                                                                                                      |
| `.railway/railway.ts` and the `railway` devDependency.                                                                                                                                                                                     | Deleted.                                                                                                                                                                                                                                                                                                         |

The readiness point deserves a sentence, because the manifests left it off on
purpose — "so a Kafka blip does not become an outage". That reasoning is right
for **liveness**, which stays dumb. Readiness is a different question: not "is
this process wedged" but "should the load balancer send it traffic". It covers
only this pod's own prerequisites — started, JWKS loaded, not on its way out —
and never a shared dependency such as trip, whose bad minute would otherwise
drain every gateway at once.

**Auth stays at one replica.** Its sign-in rate limiter keeps its counts in
memory, and for a six-digit code that limiter is the security boundary: two
replicas would each allow the full rate. Auth scales only once the limiter has
a shared store, which is the one real reason for Memorystore in Phase 4.

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
- **One shared ConfigMap.** Fixed in Phase 0: each workload has its own,
  generated from its own file under `deploy/k8s/base/config/`, and the few
  genuinely shared values (`KAFKA_*`, tracing) are the only thing in
  `surge-shared`.
- **`backend/shared/`.** A change there should redeploy the services that import
  the changed package, and only those. `go list -deps ./services/<svc>/cmd`
  lists each service's packages exactly. On the TypeScript side
  `pnpm --filter "...[origin/main]"` does the same for auth and web.

Same digest, no rollout, only works if builds are **reproducible**. Most of that
is already true: the Go Dockerfile builds with `-trimpath`, and `.dockerignore`
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

Built as `.github/workflows/ci.yml`:

| Job         | Runs when            | What                                                                                                                                   |
| ----------- | -------------------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| `changes`   | always               | `dorny/paths-filter`, and `scripts/affected.mjs`: `go list -deps` per Go service and the workspace graph for auth and web              |
| `ts`        | TS, proto            | dprint, `pnpm check`, oxlint, **`pnpm coverage`** against a ratchet                                                                    |
| `go`        | Go, proto            | gofmt, `go vet`, `go test -race -cover` with Postgres and a Redpanda broker, per-package coverage floors, `staticcheck`, `govulncheck` |
| `codegen`   | proto, Go, TS        | `make proto`, `make wire-fixtures` and the route tree, then nothing may differ from the commit                                         |
| `manifests` | `deploy/`            | `kubectl kustomize` every overlay, piped through `kubeconform`                                                                         |
| `compat`    | proto, `shared/wire` | `buf breaking` against `main`; the golden `geo.events` fixtures from `main` decode on the branch                                       |
| `images`    | affected services    | build only those images, and fail on a fixable HIGH or CRITICAL CVE; pushing arrives with the registry in Phase 2                      |
| `e2e`       | web, backend, proto  | Phase 3                                                                                                                                |
| `ci-ok`     | always               | the one required check: every job above passed or had nothing to do                                                                    |

Where it departs from the plan, and why:

- **Coverage is a ratchet, not 80% yet.** Measured on CI the day the gate
  arrived, the TypeScript suite covered 57.7% of lines, 36.1% of functions and
  33.7% of branches, and the Go packages ranged from 94% to nothing — fourteen
  have no tests at all. A gate that fails every run is a gate nobody keeps. So
  the thresholds in `vitest.config.ts` are those numbers less a point, and every
  Go package has a floor in `backend/coverage-floors.txt`, enforced by
  `scripts/go-coverage-gate.sh`. A Go package not listed is new, and held to
  80%. Floors rise and never fall; the gate says when one can. Both were set
  from runs with only what CI has — a laptop with the whole stack running
  measures higher, because the integration tests stop skipping.
- **The auth migration is checked, not regenerated.** `compileAuthMigrations`
  diffs against a live database, and `0002_phone_number.sql` extends
  `0001_auth.sql` by hand, so regenerating `0001` has nothing to be compared
  with. Instead `apps/auth/test/iam/Migrations.test.ts` applies every migration
  and asserts better-auth has nothing left to create.
- **No separate integration job.** The `go` job carries Postgres and a
  Redpanda broker, so the Kafka tests run rather than skip; the TypeScript
  database tests already had testcontainers. What still needs auth, the gateway
  and the whole stack running is Phase 3.
- **`geo.events` has golden fixtures now**, one per variant, written by
  `make wire-fixtures` beside the WebSocket ones. Until they reach `main`, the
  compat job has nothing on the base to decode and says so.
- **The Node images lost their package managers.** Trivy found that the
  `node:22-alpine` base's own npm, yarn and corepack carried most of its
  CVEs, and the runtime stages never use them. They are deleted there, and the
  OS packages take Alpine's published fixes.

`gofmt` stays the style guide. `staticcheck` and `govulncheck` are there for
correctness and known CVEs, not taste — which is why this is not
golangci-lint with forty linters switched on.

**The drift check needs reproducible codegen first.** The four protoc plugins
are `tool` directives in `backend/go.mod`, so `go.sum` pins them, and
`make proto` builds them into `backend/bin` and puts them first on `PATH` — the
old target appended `GOPATH` to it, so a Homebrew `protoc-gen-go` could win.
protoc is pinned in CI to 33.0, the version the committed code names. With
both pinned, `make proto` on a clean tree reproduces every committed file.
`flake.nix` is unchanged: nix is not installed where this was built, and a
toolchain definition nobody runs drifts without anyone noticing.

**Images.** Nine: gateway, ingest, matcher, trip, payments, simulator,
migrate, auth, web. Buildx with the GitHub Actions cache, a Trivy scan, amd64
only. h3-go is cgo, and `go.Dockerfile` already explains why that means Alpine
rather than distroless; it also means cross-building arm64 under QEMU is slow enough to
avoid. If Axion nodes are ever worth it, `ubuntu-24.04-arm` runners build them
natively.

**Hygiene.**

- `permissions: contents: read` at the top of every workflow, widened per job.
- Third-party actions pinned by commit SHA.
- **Renovate, not Dependabot**, because it understands the pnpm catalog, Go
  modules, Dockerfiles and actions in one config. `effect` and `@effect/*` are
  excluded from automatic bumps: each one is paired with a `git subtree pull`
  of `repos/effect`, and an rc bump without it is exactly the drift `AGENTS.md`
  warns about. `renovate.json` is committed; Renovate starts once its GitHub
  app is installed on the repository.
- **Branch protection on `main` requires `ci-ok`** — a repository setting, not
  a file, so it is done by hand.
- lefthook for gofmt and dprint on staged files — optional, since CI is the
  real gate, and not added.

## Phase 2 — images, built once and promoted

**Artifact Registry in `europe-west4`**, pushed to through **Workload Identity
Federation**: GitHub's OIDC token is exchanged for a short-lived GCP credential,
so there is no service-account JSON key in a GitHub secret. This needs a small
first slice of Terraform — the registry, the WIF pool and a service account —
which is why it comes before the rest of the cloud.

- Images are pushed with the **git SHA** as a tag, for people to read, and
  **deployed by digest**, which is what decides whether anything rolls — see
  _One change, one rollout_. `latest` is never deployed.
- **Builds are reproducible, and that was measured, not assumed.** Every
  timestamp is the commit's (`SOURCE_DATE_EPOCH`, and buildx's
  `rewrite-timestamp`). Two uncached builds of the gateway came out with the
  same manifest digest, `sha256:5c9918fc…`, so an unchanged service really
  does keep its digest and does not roll.
- **No attestations in the pushed index, for now.** `build-push-action` would
  add provenance and an SBOM inside the image index, and both carry the build's
  own time: the index digest would change on every build and every service would
  roll on every deploy. They come back as cosign attestations attached by
  reference, which leave the image's digest alone, with Binary Authorization
  after them.
- **Web gets its URLs at runtime** — done in Phase 0. Without it, staging and
  production would need two web builds, and the image that was tested would not
  be the image that ships.

**Built, not applied.** `deploy/terraform/gcp/bootstrap/` is the registry, the
Workload Identity pool — which refuses every repository but this one, and lets
only `main` impersonate the pusher — and a service account that can push and do
nothing else. It passes `tofu validate`; applying it needs a GCP project. Its
outputs become three repository variables, `REGISTRY`, `GCP_WIF_PROVIDER` and
`GCP_PUSH_SA`, and from then on the `images` job pushes every affected image
from `main` after its scan passes, and uploads each digest as an artifact for
the deploy to pin. Until they are set it builds, scans and pushes nothing.

## Phase 3 — Playwright against the whole stack

The tests worth having here are the ones that cross every boundary at once: a
browser, better-auth, the token exchange, the gateway, Kafka, the matcher and
trip, and back out over a WebSocket. Mocking the gateway would skip exactly the
part this project is about.

**Where it lives: `e2e/` at the root**, as `@surge/e2e` in
`pnpm-workspace.yaml` and the tsconfig references. Not `apps/e2e` —
`vitest.config.ts` has `projects: ["apps/*"]`, and Vitest adopts a matching
folder even without a config, then runs its `*.spec.ts` files as Vitest tests.

**The stack is the images this commit builds.** The compose `app` profile runs
auth, web and every Go service from their images beside the infrastructure
compose already has, with both migrators as one-shot containers first.
`docker-bake.hcl` names all nine images once; the `e2e` job builds from it,
reading the layers the `images` job cached, so it tests the artifacts that ship
and not a second recipe. Locally: `make images`, `make app-up`, `make e2e`.

**Valhalla has a pre-baked image**, `deploy/docker/valhalla.Dockerfile`: the
stock image with Noord-Holland's tiles built at image build time, then the
extract and the loose tiles dropped. It builds in three and a half minutes,
weighs 782 MB, and routes the moment it starts — a 13.6 km trip from Centraal
to Zuid was its first answer. `.github/workflows/valhalla.yml` rebuilds it
weekly and publishes it to GitHub's registry with the workflow's own token, so
no cloud account is needed; the `e2e` job pulls it, or builds it from cache
until the first one is published. The cluster's copy is promoted into Artifact
Registry by digest like every other image.

**The tests**, six of them in `e2e/tests`:

- a stranger is sent to sign in; a rider signs up, signs out and signs back in
  as the same rider; a driver signs up as a driver
- a rider and a driver in **two browser contexts**: the driver goes online at
  Centraal, the rider saves the fake processor's test card, books the
  "Centraal → Rijksmuseum" preset, and both screens follow the trip through
  accepted, arrived, started and completed
- a rider is turned away from the console; ops see the fleet and turn the
  simulator's knob

Where they depart from the plan: the ops role is granted inside the test, with
the statement `make grant-ops` runs, because no screen does it. The fleet is
read from the console's numbers rather than its map, which needs WebGL a
headless browser may not have. And the stack starts with **no simulated
drivers**: one near the pickup would take the offer meant for the test's own
driver, so the console test adds five hundred and takes them away again. The
rider never touches the map — the presets set both points — and the driver's
position is granted geolocation, the same path "Use my location" takes.

**Rules carried over from `RULES.md`.** Playwright's auto-waiting locators and
`expect.poll`, never a sleep; the trip test's ordering — driver online before
the rider books — is what makes the driver matchable, not a wait. A unique user
per test. One worker, because the stack is one city with one matcher. No
retries: a flaky end-to-end test is a finding. On failure: the trace, the
video, and every service's logs as artifacts.

**What its first runs found**, before it passed — the reason it exists:

- **The web app could not get a gateway token in any browser.** `fetch` sends
  cookies only to its own origin, and auth is never the page's origin —
  another port locally, another subdomain deployed — so the token exchange
  reached auth without the session cookie, got a 401, and every product call
  failed. Fixed: the web platform sends credentials
  (`apps/web/src/atom/platform.ts`); the gateway already answered credentialed
  requests for the web origin.
- **The console could not rescale the simulator from a browser.** The gateway's
  CORS allowed `GET` and `POST`, and `sim.proto` declares `PUT /v1/simulator`,
  so the preflight failed. Fixed, with the gateway's first CORS tests.
- **Valhalla's health check had never passed** — see the Phase 0 table.

Two more are decisions rather than fixes, and have to be made before auth faces
the internet:

- **Rate limiting has no trustworthy client address.** better-auth's built-in
  limiter finds none and falls back to one shared bucket per path: deployed,
  that is every user's sign-in in one bucket, and one caller can lock out
  everyone. The app's own limiter (`apps/auth/src/iam/AuthHttp.ts`) keys on the
  first `X-Forwarded-For` entry, which the client writes itself: a new value per
  request is a new bucket, and the limit on a six-digit code is gone. Both need
  the address the load balancer vouches for — behind Google's, the entry it
  appends, not the one the client sent — configured once, in Phase 4. The tests
  stand in for that load balancer: each browser has its own address, on
  requests to auth only (`e2e/support/client-address.ts`).
- **`QueryError` reports every typed failure as "Your role does not allow you
  to see …"**, a missing session included. That is how the token bug above
  looked like a permissions problem.

**Not yet done.** The `client-integration` Vitest project and
`backend/shared/authz` could run against the same stack instead of skipping,
and `pnpm capture:api-fixtures` could be diffed against the committed fixtures;
neither is wired in. It runs on changes to web, backend, proto or deploy, and
not yet nightly on `main`. And the suite has not run on a laptop: this one runs
the development stack under the same compose project and ports, so its first
real run is CI's.

## Phase 4 — GKE

**GKE Standard, regional, `europe-west4`**, with staging and production as
separate GCP projects. Standard over Autopilot because two things here need
control over nodes: Redpanda wants its own pool and local SSD, and the gateway's
connection density wants raised ulimits and sysctls.

| Locally          | On GKE                                                                                       |
| ---------------- | -------------------------------------------------------------------------------------------- |
| Postgres         | Cloud SQL for Postgres 17, private IP, IAM auth; `surge` and `surge_auth`                    |
| Redpanda         | Redpanda's Helm chart in-cluster: three brokers, RF 3, a dedicated node pool                 |
| —                | Memorystore, once auth needs a shared rate-limit store to run a second replica               |
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
  — done in Phase 0
- requests set from the benchmarks, not guessed
- `topologySpreadConstraints` across zones; the PDBs and HPAs that exist
- NetworkPolicies: only the gateway takes ingress; only trip talks to Postgres
- the gateway's `preStop` delay (Phase 0) is tuned so the ALB stops routing to
  it before it closes sockets — and clients must reconnect regardless, because
  the ALB puts a ceiling on how long a WebSocket lives
- the matcher rolls with `maxUnavailable: 1` (Phase 0) and never runs more
  replicas than `geo.events` has partitions; a replica past that owns nothing

## Phase 5 — infrastructure as code, and delivery

**OpenTofu in `deploy/terraform/gcp/`**, state in a GCS bucket. Modules for
project APIs, the VPC, GKE, Artifact Registry, Cloud SQL, the WIF pool, DNS, and Secret Manager secrets as **containers only** — values are set
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

- [x] **P0** fix the defects above, including per-service ConfigMaps; delete
      Railway
- [x] **P1** the CI split, coverage gates, pinned codegen and the drift check,
      manifest validation, `buf breaking` and `geo.events` compatibility,
      affected-service detection, Renovate
- [ ] **P2** Terraform bootstrap (registry, WIF), reproducible images deployed
      by digest — written and verified; waits on a GCP project to apply
- [ ] **P3** `surge-valhalla-ams`, the compose `app` profile, Playwright
- [ ] **P4** GKE, Cloud SQL, Redpanda and the `gke` component; staging deploys
      from `main`
- [ ] **P5** production behind approval, Managed Prometheus and Cloud Trace,
      cloud benchmarks; Argo Rollouts canaries for the gateway
