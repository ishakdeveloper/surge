import { configureSimulator, simulatorAtom } from "@/atom/console-atoms.js";
import { ActionError } from "@/components/app/action-error.js";
import { QueryError } from "@/components/app/query-error.js";
import { Button } from "@/components/ui/button.js";
import { Label } from "@/components/ui/label.js";
import { cn } from "@/lib/utils.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import type { Simulator } from "@surge/domain/sim/Simulator";
import { AsyncResult } from "effect/unstable/reactivity";
import * as React from "react";

/** The simulator refuses anything past this; the slider stops here to match. */
const MAX_DRIVERS = 50_000;
const INTERVALS = [1000, 2000, 4000, 8000] as const;

/** The knob: how many drivers, reporting how often. */
export const SimControls = () => {
  const simulator = useAtomValue(simulatorAtom);

  if (AsyncResult.isInitial(simulator)) {
    return <p className="text-muted-foreground text-sm">Asking the simulator…</p>;
  }
  if (AsyncResult.isFailure(simulator)) {
    return <QueryError result={simulator} subject="the simulator" />;
  }
  // Keyed by what it was told last, so a rescale from elsewhere resets the form.
  return (
    <SimForm
      key={`${simulator.value.drivers}/${simulator.value.pingIntervalMs}`}
      current={simulator.value}
    />
  );
};

const SimForm = (props: { readonly current: Simulator; }) => {
  const [drivers, setDrivers] = React.useState(props.current.drivers);
  const [interval, setInterval] = React.useState(props.current.pingIntervalMs);
  const configuring = useAtomValue(configureSimulator);
  const configure = useAtomSet(configureSimulator);

  const changed = drivers !== props.current.drivers || interval !== props.current.pingIntervalMs;
  const pingsPerSecond = Math.round(drivers / (interval / 1000));

  return (
    <section className="flex flex-col gap-3" aria-labelledby="simulator">
      <h2 id="simulator" className="text-sm font-medium">Simulator</h2>
      <p className="text-muted-foreground text-sm tabular-nums">
        {props.current.running.toLocaleString()} of {props.current.drivers.toLocaleString()}{" "}
        drivers running, {Math.round(props.current.targetPingsPerSecond).toLocaleString()}{" "}
        pings a second.
      </p>

      <div className="flex flex-col gap-2">
        <Label htmlFor="sim-drivers">Drivers: {drivers.toLocaleString()}</Label>
        <input
          id="sim-drivers"
          type="range"
          min={0}
          max={MAX_DRIVERS}
          step={500}
          value={drivers}
          onChange={(event) => {
            setDrivers(Number(event.target.value));
          }}
          className="accent-primary w-full"
        />
      </div>

      <fieldset className="flex flex-col gap-2">
        <legend className="mb-2 text-sm font-medium">Each driver reports every</legend>
        <div className="flex flex-wrap gap-2">
          {INTERVALS.map((option) => (
            <button
              key={option}
              type="button"
              aria-pressed={interval === option}
              onClick={() => {
                setInterval(option);
              }}
              className={cn(
                "rounded-md border px-3 py-1 text-sm tabular-nums",
                interval === option ? "border-primary" : "border-border",
              )}
            >
              {option / 1000}s
            </button>
          ))}
        </div>
      </fieldset>

      <p className="text-muted-foreground text-sm tabular-nums">
        That is {pingsPerSecond.toLocaleString()} pings a second into ingest.
      </p>

      {AsyncResult.isFailure(configuring) && <ActionError cause={configuring.cause} />}
      <Button
        type="button"
        disabled={!changed || configuring.waiting}
        onClick={() => {
          configure({ drivers, pingIntervalMs: interval });
        }}
      >
        {configuring.waiting ? "Rescaling…" : "Rescale"}
      </Button>
    </section>
  );
};
