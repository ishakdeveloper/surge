import { activeTripAtom, arriveTrip, completeTrip, startTrip } from "@/atom/trip-atoms.js";
import { ActionError } from "@/components/app/action-error.js";
import { Button } from "@/components/ui/button.js";
import { driverStatus, formatCents, formatDistance, formatDuration } from "@/lib/format.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { Option } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";

/**
 * The trip a driver is on, and the one thing to do next.
 *
 * Only the next step is offered. The trip service refuses them out of order —
 * no starting before arriving — so showing all three would be two buttons that
 * can only fail. Each is its own action, so a failure is reported against the
 * step that failed.
 */
export const DriverTrip = () => {
  const active = useAtomValue(activeTripAtom).pipe(AsyncResult.getOrThrow);
  const arriving = useAtomValue(arriveTrip);
  const arrive = useAtomSet(arriveTrip);
  const starting = useAtomValue(startTrip);
  const start = useAtomSet(startTrip);
  const completing = useAtomValue(completeTrip);
  const complete = useAtomSet(completeTrip);

  if (Option.isNone(active)) return null;
  const trip = active.value;

  const step = trip.status === "TRIP_STATUS_ACCEPTED"
    ? {
      label: "I have arrived",
      busy: arriving,
      run: () => {
        arrive(trip.id);
      },
    }
    : trip.status === "TRIP_STATUS_ARRIVED"
    ? {
      label: "Start the trip",
      busy: starting,
      run: () => {
        start(trip.id);
      },
    }
    : trip.status === "TRIP_STATUS_IN_PROGRESS"
    ? {
      label: "Complete the trip",
      busy: completing,
      run: () => {
        complete(trip.id);
      },
    }
    : undefined;

  return (
    <section className="flex flex-col gap-3" aria-labelledby="trip">
      <h2 id="trip" className="text-sm font-medium">Your trip</h2>

      <output
        aria-live="polite"
        className="flex flex-col gap-1 rounded-md border border-border p-4"
      >
        <p className="text-base font-medium">{driverStatus[trip.status]}</p>
        <p className="text-muted-foreground font-mono text-xs">{trip.status}</p>
      </output>

      <dl className="grid grid-cols-[5rem_1fr] gap-1.5 text-sm">
        <dt className="text-muted-foreground">Trip</dt>
        <dd className="font-mono text-xs">{trip.id}</dd>
        <dt className="text-muted-foreground">Rider</dt>
        <dd className="font-mono text-xs">{trip.riderId}</dd>
        <dt className="text-muted-foreground">Fare</dt>
        <dd className="tabular-nums">{formatCents(trip.totalCents)}</dd>
        <dt className="text-muted-foreground">Route</dt>
        <dd>{formatDistance(trip.route.meters)} · about {formatDuration(trip.route.seconds)}</dd>
      </dl>

      {step !== undefined && (
        <>
          {AsyncResult.isFailure(step.busy) && <ActionError cause={step.busy.cause} />}
          <Button type="button" disabled={step.busy.waiting} onClick={step.run}>
            {step.busy.waiting ? "Saving…" : step.label}
          </Button>
        </>
      )}
    </section>
  );
};
