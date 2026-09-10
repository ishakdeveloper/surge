#!/usr/bin/env python3
"""Prices a sample of a matcher's dispatches over real roads.

Reads the JSON lines a matcher writes with MATCHER_DISPATCH_LOG, keeps those
inside a measurement window, and asks Valhalla how long each car would take to
drive to its rider. Run after the benchmark, never during it: during a batched
run Valhalla is busy with the matcher's own travel-time matrices, and pricing
then would slow the thing being measured.

    price-dispatches.py LOG START_MS END_MS [--sample 400] [--valhalla URL]

Prints one JSON object: how many were priced, the mean, median and p95 road
seconds, and how many pairs Valhalla found no road between.
"""
import argparse
import json
import random
import statistics
import urllib.request


def road_seconds(valhalla, dispatch):
    body = json.dumps({
        "locations": [
            {"lat": dispatch["driverLat"], "lon": dispatch["driverLng"]},
            {"lat": dispatch["pickupLat"], "lon": dispatch["pickupLng"]},
        ],
        "costing": "auto",
        "directions_type": "none",
    }).encode()
    request = urllib.request.Request(f"{valhalla}/route", data=body, headers={"content-type": "application/json"})
    try:
        with urllib.request.urlopen(request, timeout=10) as response:
            return json.load(response)["trip"]["summary"]["time"]
    except Exception:
        return None


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("log")
    parser.add_argument("start_ms", type=int)
    parser.add_argument("end_ms", type=int)
    parser.add_argument("--sample", type=int, default=400)
    parser.add_argument("--valhalla", default="http://localhost:8002")
    args = parser.parse_args()

    dispatches = []
    with open(args.log) as lines:
        for line in lines:
            try:
                dispatch = json.loads(line)
            except ValueError:
                continue
            if args.start_ms <= dispatch["atMs"] <= args.end_ms:
                dispatches.append(dispatch)

    # Seeded, so the same log always yields the same sample.
    sample = random.Random(7).sample(dispatches, min(args.sample, len(dispatches)))
    priced = [road_seconds(args.valhalla, dispatch) for dispatch in sample]
    seconds = sorted(value for value in priced if value is not None)

    result = {"dispatches": len(dispatches), "priced": len(seconds), "unreachable": len(priced) - len(seconds)}
    if seconds:
        result.update({
            "mean_s": round(statistics.fmean(seconds), 1),
            "p50_s": round(seconds[len(seconds) // 2], 1),
            "p95_s": round(seconds[min(len(seconds) - 1, int(len(seconds) * 0.95))], 1),
            "crow_mean_m": round(statistics.fmean(d["meters"] for d in sample), 0),
        })
    print(json.dumps(result))


if __name__ == "__main__":
    main()
