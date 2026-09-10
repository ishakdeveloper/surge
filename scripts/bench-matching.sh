#!/usr/bin/env bash
# Greedy against batched matching, on the same fleet and the same demand.
#
# For each strategy: restart the matcher, wait until it owns every partition,
# start the fleet and wait until riders are actually asking, warm up, then
# measure a window. Both runs share one consumer group, so the second resumes
# where the first stopped. The riders' generator is seeded, so both runs see the
# same pickups in the same order.
#
#   make bench-matching DRIVERS=300 RPS=20 MINUTES=4
#
# Long enough that it should not run inside anything that can be killed for
# memory: start it detached (nohup ... &) and watch its output.
#
# A run is only reported as valid if the matcher kept up and kept its
# partitions: an earlier attempt compared a batched run against a greedy run
# whose matcher had lost its partitions and saw demand for 100 of 240 seconds.
#
# Pickup time is priced over real roads after both runs, from the positions the
# matcher logged — the fair comparison, since batching optimises road time and
# greedy optimises straight-line distance. Don't run anything else against
# Redpanda or Valhalla while this is going.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
DRIVERS=${DRIVERS:-300}
RPS=${RPS:-20}
MINUTES=${MINUTES:-4}
WARMUP=${WARMUP:-60}
PROM=${PROM:-http://localhost:9090}
VALHALLA=${VALHALLA:-http://localhost:8002}
WINDOW="${MINUTES}m"

cd "$ROOT/backend"
set -a; . ../.env; set +a
# A fresh group for every invocation, shared by its two runs. The second run
# resumes where the first stopped; the first starts at the end of the topic.
# A fixed name once made the first run resume from the previous invocation,
# hours earlier, and replay every request since — thousands of phantom trips
# offered to a fleet that was meant to be measured.
export MATCHER_GROUP=matcher-bench-$(date +%s)

# Memory. What the benchmark does not read is stopped for its length and started
# again on the way out, however it exits: the dashboards — Grafana, Jaeger,
# Redpanda Console — are a third of a gigabyte on a laptop where the memory
# guard once killed a run halfway through. Prometheus stays; the results are
# read from it. BENCH_KEEP_DASHBOARDS=1 leaves them running.
compose="docker compose -f $ROOT/deploy/compose/docker-compose.yml --profile observability"
if [ -z "${BENCH_KEEP_DASHBOARDS:-}" ]; then
  $compose stop grafana jaeger redpanda-console >/dev/null 2>&1 || true
  trap '$compose start grafana jaeger redpanda-console >/dev/null 2>&1 || true' EXIT
fi

stop() {
  pkill -f "\./bin/$1" 2>/dev/null || true
  for _ in $(seq 40); do pgrep -f "\./bin/$1" >/dev/null || return 0; sleep 0.5; done
  pkill -9 -f "\./bin/$1" 2>/dev/null || true
}

query() {
  curl -s --data-urlencode "query=$1" --data-urlencode "time=$2" "$PROM/api/v1/query" |
    python3 -c 'import json,sys; r=json.load(sys.stdin)["data"]["result"]; v=float(r[0]["value"][1]) if r else float("nan"); print("-" if v!=v else f"{v:.2f}")'
}

wait_for() { # file pattern count timeout
  for _ in $(seq "$4"); do
    [ "$(grep -c "$2" "$1" 2>/dev/null || true)" -ge "$3" ] && return 0
    sleep 1
  done
  return 1
}

# Indexed, not associative: macOS ships bash 3.2, which has no declare -A.
# STRATEGIES=batched runs one strategy alone, for when two runs back to back
# will not fit — the memory guard once killed a run halfway through its second.
strategies=(${STRATEGIES:-greedy batched})
started=() ended=() valid=() rows=()
for i in "${!strategies[@]}"; do
  strategy=${strategies[$i]}
  stop simd
  stop matcher

  mlog=/private/tmp/bench-matcher-$strategy.log
  slog=/private/tmp/bench-simd-$strategy.log
  dispatches=/private/tmp/bench-dispatch-$strategy.jsonl
  MATCH_STRATEGY=$strategy MATCHER_DISPATCH_LOG=$dispatches nohup ./bin/matcher >"$mlog" 2>&1 &
  wait_for "$mlog" 'shard restored' 32 180 || echo "[$strategy] matcher never restored all 32 partitions"

  SIM_TRANSPORT=ws SIM_DRIVERS=$DRIVERS SIM_REQUESTS_PER_SECOND=$RPS \
    SIM_RIDER_GROUP=riders-bench-$strategy-$(date +%s) \
    nohup ./bin/simd >"$slog" 2>&1 &
  # The simulator fills its route pool from Valhalla before any rider asks;
  # warm-up starts when demand does, not when the process does.
  wait_for "$slog" 'demand started' 1 300 || echo "[$strategy] riders never started"

  echo "[$strategy] demand started; warming up ${WARMUP}s, then measuring ${MINUTES} min"
  sleep "$WARMUP"
  started[$i]=$(date +%s)
  sleep $((MINUTES * 60))
  ended[$i]=$(date +%s)
  sleep 5

  s="strategy=\"$strategy\""
  end=${ended[$i]}
  handled=$(query "(sum(increase(surge_matcher_matched_total{$s}[$WINDOW])) + sum(increase(surge_matcher_abandoned_total{$s}[$WINDOW]))) / $((MINUTES * 60))" "$end")
  lost=$(awk -v from="$(date -r "${started[$i]}" '+%Y/%m/%d %H:%M:%S')" '($1" "$2) >= from && /partitions (lost|revoked)/' "$mlog" | wc -l | tr -d ' ')
  valid[$i]=yes
  python3 -c "import sys; sys.exit(0 if '$handled' != '-' and float('$handled') >= 0.9 * $RPS else 1)" || valid[$i]="NO: handled $handled of $RPS/s"
  [ "$lost" -gt 0 ] && valid[$i]="NO: partitions moved mid-window"

  rows[$i]="$handled | $(query "histogram_quantile(0.5, sum by (le) (increase(surge_matcher_match_latency_seconds_bucket{$s}[$WINDOW])))" "$end") | $(query "histogram_quantile(0.95, sum by (le) (increase(surge_matcher_match_latency_seconds_bucket{$s}[$WINDOW])))" "$end") | $(query "histogram_quantile(0.99, sum by (le) (increase(surge_matcher_match_latency_seconds_bucket{$s}[$WINDOW])))" "$end") | $(query "sum(increase(surge_matcher_abandoned_total{$s}[$WINDOW])) / $((MINUTES * 60))" "$end") | $(query "sum(increase(surge_matcher_rejections_total{reason=\"busy\"}[$WINDOW]))" "$end")"
  if [ "$strategy" = batched ]; then
    # Only batches contested enough to need travel times are counted here;
    # uncontested windows are dispatched without a solve and never reach it.
    rows[$i]+=" | $(query "sum(increase(surge_matcher_batch_requests_count[$WINDOW]))" "$end") | $(query "sum(increase(surge_matcher_batch_requests_sum[$WINDOW])) / sum(increase(surge_matcher_batch_requests_count[$WINDOW]))" "$end") | $(query "sum(increase(surge_matcher_batch_fetch_seconds_count{fetched=\"no_travel_times\"}[$WINDOW])) / sum(increase(surge_matcher_batch_fetch_seconds_count[$WINDOW]))" "$end")"
  else
    rows[$i]+=" | - | - | -"
  fi
  echo "[$strategy] done (valid: ${valid[$i]})"
done

stop simd
stop matcher

# Priced now, with Valhalla idle: the same sample size for both strategies.
priced=()
for i in "${!strategies[@]}"; do
  strategy=${strategies[$i]}
  priced[$i]=$(python3 "$ROOT/scripts/price-dispatches.py" "/private/tmp/bench-dispatch-$strategy.jsonl" \
    "$((started[$i] * 1000))" "$((ended[$i] * 1000))" --valhalla "$VALHALLA" |
    python3 -c 'import json,sys; r=json.load(sys.stdin); print(" | ".join(str(r.get(k, "-")) for k in ("mean_s", "p50_s", "p95_s", "crow_mean_m", "priced")))')
done

# The authoritative double-dispatch check: at most one TripMatched per trip.
# The window runs a minute past the measurement, so a trip requested at the
# end still has its match counted.
events=/private/tmp/bench-trip-events.jsonl
docker exec surge-redpanda rpk topic consume trip.events -o :end -f '%v\n' >"$events" 2>/dev/null &
consume=$!
for _ in $(seq 60); do kill -0 "$consume" 2>/dev/null || break; sleep 1; done
kill "$consume" 2>/dev/null || true
doubles=()
for i in "${!strategies[@]}"; do
  doubles[$i]=$(python3 "$ROOT/scripts/count-double-matches.py" "$events" "$((started[$i] * 1000))" "$((ended[$i] * 1000 + 60000))")
done

echo
echo "$DRIVERS drivers, $RPS requests/s, ${MINUTES} min measured per strategy after ${WARMUP}s of demand"
echo
echo "| strategy | valid | double dispatches | redelivered matches | handled/s | road pickup mean s | road p50 s | road p95 s | straight-line mean m | priced | latency p50 s | latency p95 s | latency p99 s | unmatched/s | busy refusals | contested solves | riders per solve | solves without travel times |"
echo "| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |"
for i in "${!strategies[@]}"; do
  IFS='|' read -r handled rest <<<"${rows[$i]}"
  read -r dispatched redelivered <<<"${doubles[$i]}"
  echo "| ${strategies[$i]} | ${valid[$i]} | $dispatched | $redelivered | $handled | ${priced[$i]} | $rest |"
done

# Put the everyday stack back: the matcher on the strategy .env chose, and a
# quiet fleet.
nohup ./bin/matcher >/private/tmp/m.log 2>&1 &
sleep 5
SIM_TRANSPORT=ws SIM_DRIVERS=300 SIM_REQUESTS_PER_SECOND=0 SIM_RIDER_GROUP=s-$(date +%s) \
  nohup ./bin/simd >/private/tmp/s.log 2>&1 &
