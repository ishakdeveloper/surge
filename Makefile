# Surge — ride-hailing on a geo-sharded matcher.
#
# Infrastructure runs in Docker; Go services run on the host. That is not an
# accident: the reload loop is a compile rather than an image build, and the
# 8 GB Docker VM is left to the things that actually need it.

COMPOSE := docker compose -f deploy/compose/docker-compose.yml
GO      := cd services && go
BIN     := services/bin

.DEFAULT_GOAL := help
.PHONY: help proto up down logs migrate build test check fmt dev-auth dev-sim dev-ingest dev-matcher load control stats chaos-scale chaos-kill clean nuke

help: ## Show this help
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | awk -F':.*?## ' '{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

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

# Two migrators, one convention: idempotent, ledger-free, never run at boot.
# packages/database owns better-auth's tables, services/migrations owns the
# trip tables, and neither writes to the other's.
migrate: build ## Apply database migrations (both languages)
	pnpm --filter @surge/database migrate
	@set -a; . ./.env; set +a; $(BIN)/migrate

# Codegen. The plugins are go-installed rather than vendored, matching how the
# Go toolchain expects protoc plugins to be found.
proto: ## Regenerate gRPC code from proto/
	@command -v protoc >/dev/null || { echo "protoc not installed"; exit 1; }
	@go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	@go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	PATH="$$PATH:$$(go env GOPATH)/bin" protoc --proto_path=proto \
		--go_out=services/pkg/proto --go_opt=module=github.com/ishakdeveloper/surge/pkg/proto \
		--go-grpc_out=services/pkg/proto --go-grpc_opt=module=github.com/ishakdeveloper/surge/pkg/proto \
		proto/*.proto
	cd services && gofmt -w pkg/proto

build: ## Build every Go binary
	$(GO) build -o bin/simd ./cmd/simd
	$(GO) build -o bin/ingest ./cmd/ingest
	$(GO) build -o bin/matcher ./cmd/matcher
	$(GO) build -o bin/trip ./cmd/trip
	$(GO) build -o bin/migrate ./cmd/migrate

test: ## Run both test suites
	$(GO) vet ./... && cd services && go test ./...
	TEST_DB_URL=postgresql://surge:surge@localhost:55433/surge pnpm test

check: ## Typecheck, lint and format-check the TypeScript
	pnpm check && pnpm lint && pnpm format:check

fmt: ## Format everything
	cd services && gofmt -w .
	pnpm format

dev-auth: ## Run the auth service (the only Node in any request path)
	pnpm --filter @surge/auth dev

dev-ingest: build ## Run location ingest
	@set -a; . ./.env; set +a; \
	INGEST_GROUP=$${INGEST_GROUP:-ingest} $(BIN)/ingest

dev-matcher: build ## Run a matcher instance (run several; they share the partitions)
	@set -a; . ./.env; set +a; \
	MATCHER_METRICS_ADDR=$${MATCHER_METRICS_ADDR:-:9103} $(BIN)/matcher

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
