#!/usr/bin/env bash
# Holds every Go package to a coverage floor.
#
# RULES.md sets 80%, and nothing checked it: most packages were written before
# any gate existed, and several are far below. Failing them all on the first run
# would be a gate nobody keeps. So each existing package has a floor at what it
# had when the gate arrived, recorded in backend/coverage-floors.txt, and CI
# fails if it drops below that. A package not listed is new, and new code meets
# the rule: 80%.
#
# Raise a floor whenever a package's coverage passes it, and never lower one.
# The gate prints which floors have room to rise.
#
# Entrypoints, tools and generated protobuf are not gated.
#
#   go test -cover ./... | scripts/go-coverage-gate.sh backend/coverage-floors.txt
#   go test -cover ./... | scripts/go-coverage-gate.sh --write backend/coverage-floors.txt
set -euo pipefail

write=false
if [[ "${1:-}" == "--write" ]]; then
  write=true
  shift
fi
floors=${1:?usage: go test -cover ./... | $0 [--write] <floors-file>}

# One line per package: "<import path> <percent>". Packages with tests report
# on an `ok` line; packages without any report 0.0% on a line of their own.
measured=$(awk '
  /coverage:/ {
    pct = $0; sub(/.*coverage: /, "", pct); sub(/%.*/, "", pct)
    pkg = ($1 == "ok") ? $2 : $1
    if (pkg ~ /\/cmd$/ || pkg ~ /\/tools\// || pkg ~ /\/shared\/proto\//) next
    print pkg, pct
  }
' | sort -u)

if [[ -z "$measured" ]]; then
  echo "no coverage in the input: run go test with -cover" >&2
  exit 1
fi

if $write; then
  {
    echo "# Coverage floors, per package. Raise one when its package passes it;"
    echo "# never lower one. A package missing here is held to 80%."
    echo "# Written by scripts/go-coverage-gate.sh --write."
    # A point of headroom, so a branch that runs one way or the other across
    # runs does not flake the gate.
    awk '{ floor = int($2) - 1; if (floor < 0) floor = 0; print $1, floor }' <<<"$measured"
  } >"$floors"
  echo "wrote $(wc -l <<<"$measured" | tr -d ' ') floors to $floors"
  exit 0
fi

awk -v floors="$floors" '
  BEGIN {
    while ((getline line < floors) > 0) {
      if (line ~ /^#/ || line ~ /^[[:space:]]*$/) continue
      split(line, field, " ")
      floor[field[1]] = field[2]
    }
  }
  {
    pkg = $1; pct = $2 + 0
    want = (pkg in floor) ? floor[pkg] + 0 : 80
    if (pct < want) {
      printf "FAIL %s: %.1f%% is below its floor of %d%%\n", pkg, pct, want
      failed = 1
    } else if ((pkg in floor) && pct >= want + 5) {
      printf "note %s: %.1f%%, floor %d%% can rise\n", pkg, pct, want
    }
    seen[pkg] = 1
  }
  END {
    for (pkg in floor) if (!(pkg in seen)) printf "note %s has a floor but no longer exists\n", pkg
    exit failed
  }
' <<<"$measured"
