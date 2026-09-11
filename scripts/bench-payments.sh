#!/usr/bin/env bash
# What holding the fare adds to a booking: the same trips booked with
# TRIP_REQUIRE_PAYMENT off, then on, each timed from the booking call to the
# MatchRequested trip writes to geo.events.
#
#   make bench-payments RPS=5 DURATION=2m
#
# Needs Redpanda, Postgres and Valhalla (quotes are routed). No matcher and no
# fleet: dispatch is the moment trip asks the matcher, and payments sits in
# front of that. Payments runs on the fake processor, so a hold costs the
# system's time rather than Stripe's — and five bookings a second would meet
# Stripe's test-mode rate limit before it met anything of ours.
#
# trip is restarted for each run and put back on .env's settings afterwards,
# however this exits; payments is stopped again unless it was already running.
# Every booked trip is cancelled at the end of its run, releasing its hold.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
RPS=${RPS:-5}
DURATION=${DURATION:-2m}
RIDERS=${RIDERS:-50}
PROM=${PROM:-http://localhost:9090}
# One seed for both runs, so both book the same pickups in the same order.
SEED=${SEED:-$(date +%s)}

cd "$ROOT/backend"
set -a; . ../.env; set +a
export TRIP_GRPC_ADDR=${TRIP_GRPC_ADDR:-localhost:8110}
export PAYMENTS_GRPC_ADDR=${PAYMENTS_GRPC_ADDR:-localhost:8112}
go build -o bin/bench-payments ./tools/bench-payments

stop() {
  pkill -f "\./bin/$1" 2>/dev/null || true
  for _ in $(seq 40); do pgrep -f "\./bin/$1" >/dev/null || return 0; sleep 0.5; done
  pkill -9 -f "\./bin/$1" 2>/dev/null || true
}

listening() {
  for _ in $(seq 60); do nc -z localhost "$1" 2>/dev/null && return 0; sleep 0.5; done
  echo "nothing came up on :$1" >&2
  return 1
}

# A group with lag is a backlog the next run would measure instead of itself.
settled() {
  for _ in $(seq 120); do
    [ "$(docker exec surge-redpanda rpk group describe "$1" 2>/dev/null | awk '/^TOTAL-LAG/{print $2}')" = 0 ] && return 0
    sleep 1
  done
  echo "consumer group $1 never caught up" >&2
  return 1
}

query() { # promql time [scale]
  curl -s --data-urlencode "query=$1" --data-urlencode "time=$2" "$PROM/api/v1/query" |
    python3 -c 'import json,sys; r=json.load(sys.stdin)["data"]["result"]; v=float(r[0]["value"][1])*float(sys.argv[1]) if r else float("nan"); print("-" if v!=v else f"{v:.0f}")' "${3:-1}"
}

payments_was_running=no
pgrep -f '\./bin/payments' >/dev/null && payments_was_running=yes
restore() {
  stop trip
  nohup ./bin/trip >/private/tmp/trip.log 2>&1 &
  [ "$payments_was_running" = yes ] || stop payments
}
trap restore EXIT

bench() { # held
  BENCH_HELD=$1 BENCH_RPS=$RPS BENCH_DURATION=$DURATION BENCH_RIDERS=$RIDERS BENCH_SEED=$SEED \
    BENCH_OUT=/private/tmp/bench-payments-$1.json ./bin/bench-payments
}

# Without the hold: trip dispatches inside the booking call, and payments is
# not running to hold anything.
stop payments
stop trip
TRIP_REQUIRE_PAYMENT=false nohup ./bin/trip >/private/tmp/bench-trip-unheld.log 2>&1 &
listening 8110
settled trip
bench false

# With it. The unheld run's bookings are payments' backlog; they are settled
# before the clock starts.
stop trip
PAYMENTS_PROCESSOR=fake nohup ./bin/payments >/private/tmp/bench-payments.log 2>&1 &
TRIP_REQUIRE_PAYMENT=true nohup ./bin/trip >/private/tmp/bench-trip-held.log 2>&1 &
listening 8110
listening 8112
settled payments
settled trip
started=$(date +%s)
bench true
ended=$(date +%s)

# The relays' own view of the held run, from Prometheus: how long a committed
# fact waited to be published. Blank if Prometheus is not scraping payments.
window="$((ended - started))s"
echo
echo "| outbox | p50 ms | p99 ms |"
echo "| --- | ---: | ---: |"
for service in trip payments; do
  echo "| $service | $(query "histogram_quantile(0.5, sum by (le) (increase(surge_${service}_outbox_lag_seconds_bucket[$window])))" "$ended" 1000) | $(query "histogram_quantile(0.99, sum by (le) (increase(surge_${service}_outbox_lag_seconds_bucket[$window])))" "$ended" 1000) |"
done
echo
echo "payments retries in the held run: $(query "sum(increase(surge_payments_fact_retries_total[$window]))" "$ended")"
