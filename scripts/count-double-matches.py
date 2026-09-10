#!/usr/bin/env python3
"""Counts trips matched more than once inside a window of trip.events.

The authoritative double-dispatch check, and it has to tell two things apart:

- The same trip matched to two different drivers is a double dispatch: two
  cars sent for one rider. That is the failure the sharding exists to prevent,
  and the number that must be zero.
- The same trip matched twice to the same driver is a redelivered fact. The
  matcher is at-least-once: if it stops before committing its last offsets, the
  next one replays them, and an accept it had already handled is handled again.
  The trip service applies a match once however often it hears of it, so this
  costs nothing — but it is worth seeing, because it counts unclean handovers.

The simulator's own detector is looser than either: it counts a second offer
after a driver's accept, which includes a late accept the matcher correctly
refused.

    count-double-matches.py EVENTS START_MS END_MS

EVENTS is trip.events as JSON lines (`rpk topic consume trip.events -o :end`).
Prints "<double dispatches> <redelivered matches>".
"""
import collections
import json
import sys


def main():
    path, start, end = sys.argv[1], int(sys.argv[2]), int(sys.argv[3])
    drivers = collections.defaultdict(list)
    with open(path) as lines:
        for line in lines:
            try:
                event = json.loads(line)
            except ValueError:
                continue
            if event.get("_tag") == "TripMatched" and start <= event.get("atMs", 0) <= end:
                drivers[event["tripId"]].append((event.get("matched") or {}).get("driverId"))
    repeated = [matched for matched in drivers.values() if len(matched) > 1]
    double_dispatches = sum(1 for matched in repeated if len(set(matched)) > 1)
    print(double_dispatches, len(repeated) - double_dispatches)


if __name__ == "__main__":
    main()
