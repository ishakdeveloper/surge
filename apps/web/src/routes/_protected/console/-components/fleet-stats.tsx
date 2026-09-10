import { fleetAtom, type FleetSample } from "@/atom/console-atoms.js";
import { useAtomValue } from "@effect/atom-react";
import { AsyncResult } from "effect/unstable/reactivity";

const latency = (ms: number) =>
  ms === 0 ? "-" : ms < 1000 ? `${ms} ms` : `${(ms / 1000).toFixed(1)} s`;
const rate = (perSecond: number) => `${perSecond.toFixed(1)}/s`;

/** The fleet's numbers, and match latency over the last two minutes. */
export const FleetStats = () => {
  const fleet = useAtomValue(fleetAtom);
  if (!AsyncResult.isSuccess(fleet)) return null;
  const { stats } = fleet.value.latest;

  const rows: ReadonlyArray<readonly [string, string]> = [
    ["Drivers", stats.drivers.toLocaleString()],
    ["Idle", stats.idle.toLocaleString()],
    ["Waiting riders", stats.pending.toLocaleString()],
    ["Open offers", stats.offers.toLocaleString()],
    ["Matched", rate(stats.matchedPerSecond)],
    ["Unmatched", rate(stats.abandonedPerSecond)],
    [
      "Surge",
      stats.maxMultiplier > 1
        ? `up to ×${stats.maxMultiplier.toFixed(1)}, ${stats.surgingCells} ${
          stats.surgingCells === 1 ? "cell" : "cells"
        }`
        : "none",
    ],
  ];

  return (
    <section className="flex flex-col gap-3" aria-labelledby="fleet">
      <h2 id="fleet" className="text-sm font-medium">Fleet</h2>
      <dl className="grid grid-cols-2 gap-x-4 gap-y-1.5 text-sm">
        {rows.map(([label, value]) => (
          <div key={label} className="flex justify-between gap-2">
            <dt className="text-muted-foreground">{label}</dt>
            <dd className="tabular-nums">{value}</dd>
          </div>
        ))}
      </dl>

      <h3 className="text-sm font-medium">Match latency, request to acceptance</h3>
      <p className="text-sm tabular-nums">
        p50 {latency(stats.p50Ms)} · p95 {latency(stats.p95Ms)} ·{" "}
        <span className="text-amber-500">p99 {latency(stats.p99Ms)}</span>
      </p>
      <LatencyChart history={fleet.value.history} />
    </section>
  );
};

const WIDTH = 240;
const HEIGHT = 64;

/**
 * Three lines in one SVG, hidden from assistive technology: the numbers it
 * draws are the ones printed above it. A charting library would be a dependency for what is
 * a hundred and twenty points redrawn once a second.
 */
const LatencyChart = (props: { readonly history: ReadonlyArray<FleetSample>; }) => {
  if (props.history.length < 2) {
    return <p className="text-muted-foreground text-xs">The chart fills in as frames arrive.</p>;
  }
  const ceiling = Math.max(1000, ...props.history.map((sample) => sample.p99Ms));
  const line = (pick: (sample: FleetSample) => number) =>
    props.history.map((sample, index) =>
      `${(index / (props.history.length - 1)) * WIDTH},${
        HEIGHT - (pick(sample) / ceiling) * HEIGHT
      }`
    ).join(" ");

  return (
    <figure className="flex flex-col gap-1">
      <svg
        viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
        className="h-16 w-full rounded-md border border-border"
        preserveAspectRatio="none"
        aria-hidden
      >
        <polyline
          points={line((s) => s.p50Ms)}
          fill="none"
          className="stroke-foreground"
          strokeWidth={1}
        />
        <polyline
          points={line((s) => s.p95Ms)}
          fill="none"
          className="stroke-muted-foreground"
          strokeWidth={1}
        />
        <polyline
          points={line((s) => s.p99Ms)}
          fill="none"
          className="stroke-amber-500"
          strokeWidth={1.5}
        />
      </svg>
      <figcaption className="text-muted-foreground text-xs">Scale to {latency(ceiling)}</figcaption>
    </figure>
  );
};
