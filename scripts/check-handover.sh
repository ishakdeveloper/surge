#!/usr/bin/env bash
# Does a matcher that stops cleanly commit everything it processed?
#
# Starts the matcher on a fresh consumer group, lets the fleet and a trickle of
# riders run, stops the fleet so geo.events goes quiet, then stops the matcher
# gracefully and reads the group's committed offsets. A clean handover leaves
# zero lag: the next owner resumes exactly where the checkpoint was taken, and
# replays nothing. Any lag is work the next owner will do a second time — which
# is how a benchmark once saw trips matched twice at a handover.
#
#   make check-handover
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT/backend"
set -a; . ../.env; set +a
export MATCHER_GROUP=handover-check-$(date +%s)

stop() {
  pkill -"${2:-TERM}" -f "\./bin/$1" 2>/dev/null || true
  for _ in $(seq 60); do pgrep -f "\./bin/$1" >/dev/null || return 0; sleep 0.5; done
  echo "$1 did not stop within 30s"; return 1
}
wait_for() { # file pattern count timeout
  for _ in $(seq "$4"); do
    [ "$(grep -c "$2" "$1" 2>/dev/null || true)" -ge "$3" ] && return 0
    sleep 1
  done
  echo "timed out waiting for '$2' in $1"; return 1
}

stop simd
stop matcher

nohup ./bin/matcher >/private/tmp/handover-matcher.log 2>&1 &
wait_for /private/tmp/handover-matcher.log 'shard restored' 32 120

SIM_TRANSPORT=ws SIM_DRIVERS=300 SIM_REQUESTS_PER_SECOND=5 SIM_RIDER_GROUP=handover-$(date +%s) \
  nohup ./bin/simd >/private/tmp/handover-simd.log 2>&1 &
wait_for /private/tmp/handover-simd.log 'demand started' 1 120
sleep 20

# Quiet the topic, then stop the matcher the moment it is quiet. Ingest drains
# loc.ping for a moment after the fleet stops, so wait until geo.events stops
# growing — and then no longer. The autocommit runs every 5s; a quiet period
# longer than that lets it commit everything and hides a shutdown that commits
# nothing, which is exactly what an earlier version of this check did.
stop simd
ends() { docker exec surge-redpanda rpk topic describe geo.events -p | awk 'NR>1 { s += $NF } END { print s }'; }
previous=$(ends)
for _ in $(seq 15); do
  sleep 1
  current=$(ends)
  [ "$current" = "$previous" ] && break
  previous=$current
done

stop matcher TERM
grep -E "partitions revoked|commit on release" /private/tmp/handover-matcher.log | tail -2 | cut -c1-120

docker exec surge-redpanda rpk group describe "$MATCHER_GROUP" | awk '
  $1 == "geo.events" { partitions++; if ($3 == "-") uncommitted++; else if ($6 > 0) { lagging++; lag += $6 } }
  END { printf "partitions %d · lagging %d · total lag %d records · never committed %d\n", partitions, lagging, lag, uncommitted }'

# Put the everyday stack back.
nohup ./bin/matcher >/private/tmp/m.log 2>&1 &
sleep 5
SIM_TRANSPORT=ws SIM_DRIVERS=300 SIM_REQUESTS_PER_SECOND=0 SIM_RIDER_GROUP=s-$(date +%s) \
  nohup ./bin/simd >/private/tmp/s.log 2>&1 &
