#!/usr/bin/env bash
# Chaos harness for the matcher.
#
# Both scenarios ask the same question: when a partition changes hands, does any
# rider end up with two drivers? `surge_sim_double_dispatch_total` is the answer
# and it must stay at zero.
#
# `scale` is the graceful path — a new instance joins, Kafka rebalances
# cooperatively, and the losing shard gets to checkpoint before letting go.
# `kill` is the ungraceful one — SIGKILL means no checkpoint at all, and
# recovery rests entirely on every hold having a deadline.
set -euo pipefail

cd "$(dirname "$0")/.."
set -a; . ./.env; set +a

SCENARIO="${1:-scale}"
BIN=backend/bin

metric() { curl -s "http://localhost:$1/metrics" | awk -v m="$2" '$1==m{print $2}'; }
matched()    { metric 9103 surge_matcher_matched_total; }
duplicates() { metric 9101 surge_sim_double_dispatch_total; }

report() {
  printf "  %-22s matched=%-8s duplicates=%-4s owned(a)=%-4s owned(b)=%s\n" \
    "$1" "$(matched)" "$(duplicates)" \
    "$(metric 9103 surge_matcher_partitions_owned)" \
    "$(metric 9113 surge_matcher_partitions_owned || echo -)"
}

echo "chaos: $SCENARIO"
report "before"

case "$SCENARIO" in
  scale)
    echo "  starting a second matcher..."
    MATCHER_METRICS_ADDR=:9113 "$BIN/matcher" > /tmp/surge-matcher-b.log 2>&1 &
    SECOND=$!
    # A cooperative rebalance takes a few seconds: the joining member waits for
    # the group to settle before any partition actually moves.
    sleep 20
    report "after join"

    echo "  stopping it again (graceful: it checkpoints on the way out)..."
    kill -TERM "$SECOND" 2>/dev/null || true
    wait "$SECOND" 2>/dev/null || true
    sleep 20
    report "after leave"
    ;;

  kill)
    echo "  starting a second matcher..."
    MATCHER_METRICS_ADDR=:9113 "$BIN/matcher" > /tmp/surge-matcher-b.log 2>&1 &
    SECOND=$!
    sleep 20
    report "after join"

    echo "  kill -9 (no checkpoint; recovery rests on offer deadlines)..."
    kill -9 "$SECOND" 2>/dev/null || true
    # The session timeout is 10s, so the group needs longer than that to notice
    # and reassign. Anything faster would be measuring the sleep.
    sleep 30
    report "after kill"
    ;;

  *)
    echo "usage: chaos.sh [scale|kill]" >&2
    exit 1
    ;;
esac

DUPES="$(duplicates)"
echo
if [ "${DUPES:-0}" = "0" ]; then
  echo "  PASS  no double dispatch through the rebalance"
else
  echo "  FAIL  $DUPES trips were dispatched twice"
  exit 1
fi

echo "  restore stalls:"
curl -s http://localhost:9103/metrics | grep -E '^surge_matcher_restore_stall_seconds_(sum|count)|^surge_matcher_restored_offers_total' | sed 's/^/    /'
