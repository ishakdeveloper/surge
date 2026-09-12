# Surge — ride-hailing on a geo-sharded matcher.
#
# Infrastructure runs in Docker; Go services run on the host. That is not an
# accident: the reload loop is a compile rather than an image build, and the
# 8 GB Docker VM is left to the things that actually need it.

COMPOSE := docker compose -f deploy/compose/docker-compose.yml
GO      := cd backend && go
BIN     := backend/bin

.DEFAULT_GOAL := help
.PHONY: help proto images k8s-up k8s-down k8s-diff k8s-status tilt scaffold wire-fixtures grant-ops bench-matching bench-payments check-handover up up-core down logs migrate build test check fmt dev-auth dev-mobile dev-mobile-build mobile-build-sim mobile-build-device e2e-mobile dev-sim dev-ingest dev-matcher load control stats chaos-scale chaos-kill clean nuke

help: ## Show this help
	@grep -hE '^[a-z0-9-]+:.*?## ' $(MAKEFILE_LIST) | awk -F':.*?## ' '{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

# --- kubernetes -------------------------------------------------------------
#
# Not the development loop. `make dev-*` runs the services on the host against
# the compose stack, which is a one-second compile instead of an image build.
# This exists because "it runs as five processes on my laptop" is a different
# claim from "it deploys", and the gap between them is where a surprising number
# of problems live.
#
# Every target pins an explicit local context. The kubectl context on this
# machine has been a production GKE cluster before now.
KCTX ?= orbstack

images: ## Build every container image
	@for s in gateway ingest matcher trip chat fleet simulator migrate; do \
		echo "  building $$s"; \
		docker build -q -f deploy/docker/$$s.Dockerfile -t surge/$$s:dev . > /dev/null; \
	done
	@echo "  building auth"
	@docker build -q -f apps/auth/Dockerfile -t surge/auth:dev . > /dev/null

K8S_ENV ?= development

k8s-up: images ## Deploy to a local cluster (K8S_ENV=development)
	@kubectl --context $(KCTX) config current-context | grep -qE '^(orbstack|docker-desktop|rancher-desktop|minikube|kind-)' \
		|| { echo "refusing: $(KCTX) is not a local cluster"; exit 1; }
	kubectl --context $(KCTX) apply -f deploy/k8s/base/namespace.yaml
	@kubectl --context $(KCTX) -n surge get secret surge-secrets >/dev/null 2>&1 || \
		kubectl --context $(KCTX) -n surge create secret generic surge-secrets \
			--from-literal=AUTH_SECRET="$$(openssl rand -base64 32)" \
			--from-literal=SIM_TOKEN_SECRET="$$(openssl rand -base64 32)" \
			--from-literal=SIM_ENABLED=true
	kubectl --context $(KCTX) apply -k deploy/k8s/$(K8S_ENV)
	@echo
	@echo "  kubectl --context $(KCTX) -n surge get pods -w"
	@echo "  valhalla builds tiles on first boot; the rest waits for it rather than crashlooping"

k8s-down: ## Remove the deployment, keep the cluster
	kubectl --context $(KCTX) delete namespace surge --ignore-not-found

k8s-diff: ## Render an environment: make k8s-diff K8S_ENV=production
	@kubectl --context $(KCTX) kustomize deploy/k8s/$(K8S_ENV)

k8s-status: ## Pods, and the shard assignment across matcher pods
	@kubectl --context $(KCTX) -n surge get pods
	@echo
	@for p in $$(kubectl --context $(KCTX) -n surge get pods -l app=matcher -o name 2>/dev/null); do \
		kubectl --context $(KCTX) -n surge exec $$p -- wget -qO- http://localhost:9103/metrics 2>/dev/null \
		| awk -v pod="$${p#pod/}" '/^surge_matcher_partitions_owned /{printf "  %-28s partitions=%s\n", pod, $$2}'; \
	done

tilt: ## Kubernetes with live rebuilds
	tilt up

scaffold: ## Create a new service: make scaffold NAME=pricing
	cd backend && go run ./tools/create-service -name $(or $(NAME),$(error set NAME))

grant-ops: ## Make an account ops, for /console: make grant-ops EMAIL=you@example.com, then sign in again
	@docker exec surge-postgres psql -U surge -d surge_auth -c \
		"update \"user\" set role = 'ops' where email = '$(or $(EMAIL),$(error set EMAIL))'"

wire-fixtures: ## Regenerate the WebSocket protocol fixtures both suites assert against
	cd backend && go run ./tools/wire-fixtures

up: ## Start all infrastructure: the system and its dashboards
	@docker context show 2>/dev/null | grep -q orbstack || \
		echo "  note: not on the orbstack context; docker desktop wedged under this workload"
	$(COMPOSE) --profile observability up -d
	@echo
	@echo "  redpanda console  http://localhost:8080"
	@echo "  grafana           http://localhost:3005"
	@echo "  prometheus        http://localhost:9090"
	@echo "  jaeger            http://localhost:16686"
	@echo "  valhalla          http://localhost:8002/status"
	@echo
	@echo "  Valhalla builds tiles on first boot: 10-20 minutes. 'make logs' to watch."

up-core: ## Only what the system needs to run (postgres, redpanda, redis, valhalla), no dashboards
	$(COMPOSE) up -d

down: ## Stop infrastructure, keep the volumes
	$(COMPOSE) --profile observability down

logs: ## Follow infrastructure logs
	$(COMPOSE) logs -f

# Two migrators, two databases, one convention: idempotent, ledger-free, never
# run at boot. packages/database owns better-auth's schema in surge_auth;
# backend/migrations owns the trip schema in surge. Neither can reach the
# other's, which is the point.
migrate: build ## Apply migrations to both databases
	pnpm --filter @surge/database migrate
	@set -a; . ./.env; set +a; $(BIN)/migrate

# Codegen. The plugins are go-installed rather than vendored, matching how the
# Go toolchain expects protoc plugins to be found.
# Codegen. Four plugins from one set of .proto files:
#
#   protoc-gen-go          the messages
#   protoc-gen-go-grpc     the service client and server
#   protoc-gen-grpc-gateway  the REST reverse proxy, from the google.api.http
#                            annotations on each RPC
#   protoc-gen-openapiv2   the OpenAPI document, from the same annotations
#
# That is the reason for the annotations: the REST surface, the gRPC contract
# and the API documentation are one source of truth rather than three that
# drift.
proto: ## Regenerate gRPC, REST gateway and OpenAPI from proto/
	@command -v protoc >/dev/null || { echo "protoc not installed"; exit 1; }
	@# Pinned to the runtime versions in backend/go.mod rather than @latest, so a
	@# regeneration changes what the proto changed and nothing else — a newer
	@# generator rewrites every file's header and helpers, and buries the diff.
	@go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
	@go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2
	@go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@v2.30.0
	@go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2@v2.30.0
	@mkdir -p docs/api
	PATH="$$PATH:$$(go env GOPATH)/bin" protoc --proto_path=proto \
		--go_out=backend/shared/proto --go_opt=module=github.com/ishakdeveloper/surge/shared/proto \
		--go-grpc_out=backend/shared/proto --go-grpc_opt=module=github.com/ishakdeveloper/surge/shared/proto \
		--grpc-gateway_out=backend/shared/proto \
		--grpc-gateway_opt=module=github.com/ishakdeveloper/surge/shared/proto \
		--grpc-gateway_opt=generate_unbound_methods=false \
		--openapiv2_out=docs/api \
		--openapiv2_opt=allow_merge=true,merge_file_name=surge,disable_default_errors=true \
		proto/trip.proto proto/driver.proto proto/common.proto proto/sim.proto proto/payments.proto proto/chat.proto \
		proto/profile.proto proto/fleet.proto
	cd backend && gofmt -w shared/proto
	# The gateway embeds the document it serves, so a rebuild cannot leave the
	# published spec describing an older API.
	cp docs/api/surge.swagger.json backend/services/gateway/internal/infrastructure/http/openapi.json
	# And the reference HttpApi the contract test diffs the hand-written client
	# against, so a field that moves in the proto fails a test rather than a page.
	pnpm generate:api

build: ## Build every Go binary
	$(GO) build -o bin/simd ./services/simulator/cmd
	$(GO) build -o bin/ingest ./services/ingest/cmd
	$(GO) build -o bin/matcher ./services/matcher/cmd
	$(GO) build -o bin/trip ./services/trip/cmd
	$(GO) build -o bin/gateway ./services/gateway/cmd
	$(GO) build -o bin/payments ./services/payments/cmd
	$(GO) build -o bin/chat ./services/chat/cmd
	$(GO) build -o bin/core ./services/core/cmd
	$(GO) build -o bin/fleet ./services/fleet/cmd
	$(GO) build -o bin/migrate ./tools/migrate

test: ## Run both test suites
	$(GO) vet ./... && cd backend && go test ./...
	TEST_DB_URL=postgresql://surge:surge@localhost:55433/surge \
	VITE_STRIPE_PUBLISHABLE_KEY=$$(sed -n 's/^VITE_STRIPE_PUBLISHABLE_KEY=//p' .env 2>/dev/null) \
	pnpm test
	pnpm --filter @surge/mobile test:screens

check: ## Typecheck, lint and format-check the TypeScript
	pnpm check && pnpm lint && pnpm format:check

fmt: ## Format everything
	cd backend && gofmt -w .
	pnpm format

dev-auth: ## Run the auth service (the only Node in any request path)
	pnpm --filter @surge/auth dev

# The phone reaches the services at the address it loaded the bundle from, so
# a device on the same network needs no configuration — see
# apps/mobile/src/lib/service-urls.ts. Stripe's publishable key is the web
# app's, passed through, so a key lives in one place.
#
# This serves the bundle to a development build, not to Expo Go: Mapbox's map
# and background location are both native modules Expo Go does not carry, so
# `make mobile-build-sim` comes first, once.
dev-mobile: ## Serve the bundle to the development build: i for the iOS simulator, a for Android
	@set -a; . ./.env; set +a; \
	EXPO_PUBLIC_STRIPE_PUBLISHABLE_KEY=$${EXPO_PUBLIC_STRIPE_PUBLISHABLE_KEY:-$$VITE_STRIPE_PUBLISHABLE_KEY} \
	pnpm --filter @surge/mobile dev

# Development builds: the app itself, with every native module, rather than
# Expo Go — which is now the only way to run it at all, because Mapbox's map
# and background location are both native. Built in Expo's cloud by EAS, so
# there is no CocoaPods to install and no signing to set up by hand. Once:
# `eas login`, then `cd apps/mobile && eas init`, and the two Mapbox tokens as
# EAS environment variables (see apps/mobile/.env.example). Install a finished
# simulator build with `eas build:run -p ios --latest`; a device build installs
# from the link EAS prints, on an iPhone registered with `eas device:create`.
mobile-build-sim: ## Build the development client for the iOS simulator, on EAS
	cd apps/mobile && eas build --profile development --platform ios

mobile-build-device: ## Build the development client for a registered iPhone, on EAS
	cd apps/mobile && eas build --profile development-device --platform ios

# The same build on this machine instead: needs Xcode and CocoaPods
# (`brew install cocoapods`), which Expo drives — no Podfile to edit.
dev-mobile-build: ## Build and run the development client on the iOS simulator, locally
	pnpm --filter @surge/mobile ios:dev

# End-to-end flows on the simulator, against the running services. Needs the
# app running (`make dev-mobile`) and the auth service started with
# AUTH_DEV_OUTBOX=true, so the flows can read the codes they type. The rider
# flow also needs trip and chat; the driver flow the gateway.
#
# `e2e/book-a-ride.yaml` is left out of the default run because it saves a
# card: with a real Stripe key that happens in Stripe's own sheet, which no
# flow may fill in. Run it against the fake processor instead:
#
#   PAYMENTS_PROCESSOR=fake make dev-payments
#   EXPO_PUBLIC_STRIPE_PUBLISHABLE_KEY= make dev-mobile
#   cd apps/mobile && ~/.maestro/bin/maestro test e2e/book-a-ride.yaml
#
# The same path against the real Stripe wants the card saved out of band,
# since nothing here types a card number. Stripe's own test payment method is
# an identifier rather than a card, so the app's SetupIntent can be confirmed
# with it and the app then finds a saved Visa waiting:
#
#   make stripe-listen                       # the webhooks are half the flow
#   curl -XPOST $SURGE/v1/payments/setup-intents -H "authorization: Bearer $T"
#   stripe setup_intents confirm seti_... -d payment_method=pm_card_visa
#
# What follows is all real: a manual-capture hold on booking
# (payment_intent.amount_capturable_updated), the capture when the driver
# completes (payment_intent.succeeded), and the release on a cancelled or
# unmatched trip (payment_intent.canceled). A completed trip needs a driver
# who is really there — the simulator's drivers accept an offer but never
# arrive, so scale them to zero (PUT /v1/simulator, ops only) and put a real
# driver on shift. A driver is on shift by the `status` in their ping: `idle`
# is online and free.
e2e-mobile: ## Run the Maestro flows in apps/mobile/e2e
	cd apps/mobile && ~/.maestro/bin/maestro test e2e/sign-up-rider-by-email.yaml e2e/sign-up-driver-by-phone.yaml

dev-ingest: build ## Run location ingest
	@set -a; . ./.env; set +a; \
	INGEST_GROUP=$${INGEST_GROUP:-ingest} $(BIN)/ingest

dev-matcher: build ## Run a matcher instance (run several; they share the partitions)
	@set -a; . ./.env; set +a; \
	MATCHER_METRICS_ADDR=$${MATCHER_METRICS_ADDR:-:9103} $(BIN)/matcher

dev-gateway: build ## Run the API gateway (REST + WebSocket on :8100)
	@set -a; . ./.env; set +a; $(BIN)/gateway

dev-trip: build ## Run the trip service (gRPC on :8110)
	@set -a; . ./.env; set +a; $(BIN)/trip

dev-payments: build ## Run the payments service (metrics on :9107)
	@set -a; . ./.env; set +a; $(BIN)/payments

# Notifications are faked unless .env chooses Expo, so a laptop never pushes to
# a real phone by accident.
dev-chat: build ## Run the chat service (gRPC on :8113)
	@set -a; . ./.env; set +a; \
	CHAT_PUSH_PROVIDER=$${CHAT_PUSH_PROVIDER:-fake} $(BIN)/chat

# Photos stay in the process unless .env chooses R2 and holds its keys, so a
# fresh clone runs without a Cloudflare account.
dev-core: build ## Run the core service: names and photos (gRPC on :8114)
	@set -a; . ./.env; set +a; \
	AVATAR_STORE=$${AVATAR_STORE:-memory} $(BIN)/core
# The register is the RDW's open data, which needs no key, so the real one is
# the default here too. Identity and the documents bucket are faked unless .env
# says otherwise.
dev-fleet: build ## Run the fleet service (gRPC on :8115)
	@set -a; . ./.env; set +a; \
	FLEET_IDENTITY_PROVIDER=$${FLEET_IDENTITY_PROVIDER:-fake} \
	FLEET_DOCUMENT_STORE=$${FLEET_DOCUMENT_STORE:-memory} \
	$(BIN)/fleet

# Stripe's events, forwarded to the gateway's webhook routes. The key comes from
# .env rather than `stripe login`, so there is one place a key lives; the
# signing secret it prints is the one STRIPE_WEBHOOK_SECRET must hold. Connect
# events arrive on the same routes, and v2 accounts report changes only as thin
# events, which need their own flags.
stripe-listen: ## Forward Stripe webhooks to the local gateway
	@set -a; . ./.env; set +a; stripe listen --api-key "$$STRIPE_SECRET_KEY" \
		--events setup_intent.succeeded,payment_intent.amount_capturable_updated,payment_intent.payment_failed,payment_intent.succeeded,payment_intent.canceled,payout.paid,payout.failed,payout.canceled,charge.dispute.created,charge.dispute.closed,refund.failed \
		--forward-to localhost:8100/webhooks/stripe \
		--forward-connect-to localhost:8100/webhooks/stripe \
		--thin-events 'v2.core.account[configuration.recipient].capability_status_updated,v2.core.account[requirements].updated' \
		--forward-thin-to localhost:8100/webhooks/stripe/thin \
		--forward-thin-connect-to localhost:8100/webhooks/stripe/thin

dev-sim: build ## Run the driver simulator
	@set -a; . ./.env; set +a; \
	SIM_DRIVERS=$${DRIVERS:-1000} $(BIN)/simd

# The knob.
#
# A fresh consumer group per run, deliberately: reusing one measures the
# previous run's backlog rather than this run's throughput, which is a mistake
# worth making exactly once (see docs/benchmarks).
load: ## Ramp the fleet: make load DRIVERS=40000
	@curl -sX POST localhost:8101/sim/config \
		-H 'content-type: application/json' \
		-d '{"drivers":$(or $(DRIVERS),10000)}' | python3 -m json.tool

control: ## Simulator-only control run, pings discarded
	@set -a; . ./.env; set +a; \
	SIM_KAFKA=false SIM_DRIVERS=$${DRIVERS:-100000} $(BIN)/simd

bench-matching: build ## Greedy vs batched matching on the same demand: make bench-matching RPS=20 MINUTES=4
	@DRIVERS=$(or $(DRIVERS),300) RPS=$(or $(RPS),20) MINUTES=$(or $(MINUTES),4) scripts/bench-matching.sh

bench-payments: build ## What holding the fare adds between booking and dispatch: make bench-payments RPS=5 DURATION=2m
	@RPS=$(or $(RPS),5) DURATION=$(or $(DURATION),2m) scripts/bench-payments.sh

check-handover: build ## Does a matcher that stops cleanly commit everything it processed? Expect zero lag
	@scripts/check-handover.sh

stats: ## Current simulator and ingest state
	@echo "sim:    $$(curl -s localhost:8101/sim/stats)"
	@echo "ingest: $$(curl -s localhost:8102/debug/stats)"

# Chaos.
#
# Both targets exist to answer one question: does a partition changing hands
# ever hand one driver to two riders? The invariant is
# surge_sim_double_dispatch_total, and it must stay at zero through both.
chaos-scale: ## Add a matcher instance under load and watch the rebalance
	@bash scripts/chaos.sh scale

chaos-kill: ## kill -9 a matcher mid-offer and watch recovery
	@bash scripts/chaos.sh kill

clean: ## Remove build output
	rm -rf $(BIN) apps/*/build packages/*/build apps/web/.output

nuke: ## Remove infrastructure AND its data
	$(COMPOSE) --profile observability down -v
