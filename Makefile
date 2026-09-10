# Surge — ride-hailing on a geo-sharded matcher.
#
# Infrastructure runs in Docker; Go services run on the host. That is not an
# accident: the reload loop is a compile rather than an image build, and the
# 8 GB Docker VM is left to the things that actually need it.

COMPOSE := docker compose -f deploy/compose/docker-compose.yml
GO      := cd backend && go
BIN     := backend/bin

.DEFAULT_GOAL := help
.PHONY: help proto images k8s-up k8s-down k8s-diff k8s-status tilt scaffold up down logs migrate build test check fmt dev-auth dev-sim dev-ingest dev-matcher load control stats chaos-scale chaos-kill clean nuke

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
	@for s in gateway ingest matcher trip simulator migrate; do \
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

up: ## Start infrastructure (redpanda, postgres, redis, valhalla, prometheus, grafana)
	@docker context show 2>/dev/null | grep -q orbstack || \
		echo "  note: not on the orbstack context; docker desktop wedged under this workload"
	$(COMPOSE) up -d
	@echo
	@echo "  redpanda console  http://localhost:8080"
	@echo "  grafana           http://localhost:3005"
	@echo "  prometheus        http://localhost:9090"
	@echo "  jaeger            http://localhost:16686"
	@echo "  valhalla          http://localhost:8002/status"
	@echo
	@echo "  Valhalla builds tiles on first boot: 10-20 minutes. 'make logs' to watch."

down: ## Stop infrastructure, keep the volumes
	$(COMPOSE) down

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
	@go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	@go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	@go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@latest
	@go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2@latest
	@mkdir -p docs/api
	PATH="$$PATH:$$(go env GOPATH)/bin" protoc --proto_path=proto \
		--go_out=backend/shared/proto --go_opt=module=github.com/ishakdeveloper/surge/shared/proto \
		--go-grpc_out=backend/shared/proto --go-grpc_opt=module=github.com/ishakdeveloper/surge/shared/proto \
		--grpc-gateway_out=backend/shared/proto \
		--grpc-gateway_opt=module=github.com/ishakdeveloper/surge/shared/proto \
		--grpc-gateway_opt=generate_unbound_methods=false \
		--openapiv2_out=docs/api \
		--openapiv2_opt=allow_merge=true,merge_file_name=surge \
		proto/trip.proto proto/driver.proto proto/common.proto
	cd backend && gofmt -w shared/proto
	# The gateway embeds the document it serves, so a rebuild cannot leave the
	# published spec describing an older API.
	cp docs/api/surge.swagger.json backend/services/gateway/internal/infrastructure/http/openapi.json

build: ## Build every Go binary
	$(GO) build -o bin/simd ./services/simulator/cmd
	$(GO) build -o bin/ingest ./services/ingest/cmd
	$(GO) build -o bin/matcher ./services/matcher/cmd
	$(GO) build -o bin/trip ./services/trip/cmd
	$(GO) build -o bin/gateway ./services/gateway/cmd
	$(GO) build -o bin/migrate ./tools/migrate

test: ## Run both test suites
	$(GO) vet ./... && cd backend && go test ./...
	TEST_DB_URL=postgresql://surge:surge@localhost:55433/surge pnpm test

check: ## Typecheck, lint and format-check the TypeScript
	pnpm check && pnpm lint && pnpm format:check

fmt: ## Format everything
	cd backend && gofmt -w .
	pnpm format

dev-auth: ## Run the auth service (the only Node in any request path)
	pnpm --filter @surge/auth dev

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
	$(COMPOSE) down -v
