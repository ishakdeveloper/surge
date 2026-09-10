import { driverPositionAtom } from "@/atom/realtime-atoms.js";
import { activeTripAtom, cancelTrip } from "@/atom/trip-atoms.js";
import { ActionError } from "@/components/app/action-error.js";
import { SplitView } from "@/components/app/split-view.js";
import { type MapMarker, type MapPoint, SurgeMap } from "@/components/map/surge-map.js";
import { Button } from "@/components/ui/button.js";
import { formatCents, formatDistance, formatDuration, riderStatus } from "@/lib/format.js";
import { HoldStep } from "@/routes/_protected/ride/-components/trip-payment.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { decodePolyline6 } from "@surge/domain/geo/Polyline";
import { isPending, isUnderway, type Trip } from "@surge/domain/trip/Trip";
import { Option, Result } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";

/**
 * A trip in flight, from the rider's side.
 *
 * Reads the active trip itself rather than being handed it, per
 * `knowledge/rules/effect-atom.md`: the parent has already guarded the loading
 * and failure states, so here it is unwrapped, and the moment a push moves the
 * trip on, this re-renders on its own.
 */
export const RiderTrip = () => {
  const active = useAtomValue(activeTripAtom).pipe(AsyncResult.getOrThrow);
  const cancelling = useAtomValue(cancelTrip);
  const cancel = useAtomSet(cancelTrip);

  // The parent swaps this out when the trip finishes; this covers the render in
  // between rather than throwing.
  if (Option.isNone(active)) return null;
  const trip = active.value;

  const route = Result.getOrElse(decodePolyline6(trip.route.polyline6), () => []);
  // The state machine allows cancelling until the rider is in the car.
  const cancellable = isPending(trip.status) || trip.status === "TRIP_STATUS_ACCEPTED"
    || trip.status === "TRIP_STATUS_ARRIVED";

  return (
    <SplitView
      aside={
        <>
          <header className="flex flex-col gap-1">
            <h1 className="text-lg font-semibold">Your trip</h1>
            <p className="text-muted-foreground font-mono text-xs">{trip.id}</p>
          </header>

          {/* Announced as it changes: this is the update a rider is waiting for. */}
          <output
            aria-live="polite"
            className="flex flex-col gap-1 rounded-md border border-border p-4"
          >
            <p className="text-base font-medium">{riderStatus[trip.status]}</p>
            <p className="text-muted-foreground font-mono text-xs">{trip.status}</p>
          </output>

          {trip.status === "TRIP_STATUS_PAYMENT_PENDING" && (
            <HoldStep tripId={trip.id} totalCents={trip.totalCents} />
          )}
          {isUnderway(trip.status) && trip.driverId !== "" && <DriverEta tripId={trip.id} />}

          <dl className="grid grid-cols-[6rem_1fr] gap-1.5 text-sm">
            <dt className="text-muted-foreground">Driver</dt>
            <dd className="font-mono text-xs">{trip.driverId === "" ? "-" : trip.driverId}</dd>
            <dt className="text-muted-foreground">Fare</dt>
            <dd className="tabular-nums">{formatCents(trip.totalCents)}</dd>
            <dt className="text-muted-foreground">Route</dt>
            <dd>
              {formatDistance(trip.route.meters)} · about {formatDuration(trip.route.seconds)}
            </dd>
          </dl>

          {AsyncResult.isFailure(cancelling) && <ActionError cause={cancelling.cause} />}
          {cancellable && (
            <Button
              type="button"
              variant="outline"
              disabled={cancelling.waiting}
              onClick={() => {
                cancel({ tripId: trip.id, reason: "rider cancelled" });
              }}
            >
              {cancelling.waiting ? "Cancelling…" : "Cancel trip"}
            </Button>
          )}
        </>
      }
    >
      {isUnderway(trip.status) && trip.driverId !== ""
        ? <FollowedMap trip={trip} route={route} />
        : <SurgeMap markers={stops(trip)} route={route} follow={[trip.pickup, trip.dropoff]} />}
    </SplitView>
  );
};

/**
 * The predicted wait while the driver is on the way, learned from pickups the
 * fleet has made. Nothing while there is no prediction, or once the rider is
 * in the car.
 */
const DriverEta = (props: { readonly tripId: Trip["id"]; }) => {
  const position = useAtomValue(driverPositionAtom(props.tripId));
  if (!AsyncResult.isSuccess(position) || position.value.etaSeconds <= 0) return null;
  return (
    <p className="text-sm">
      Arriving in about {formatDuration(position.value.etaSeconds)}
    </p>
  );
};

const stops = (trip: Trip): ReadonlyArray<MapMarker> => [
  { id: "pickup", position: trip.pickup, kind: "pickup" },
  { id: "dropoff", position: trip.dropoff, kind: "dropoff" },
];

/**
 * The map once a driver is assigned, with the driver on it.
 *
 * Its own component because following is a subscription: mounting this asks
 * the gateway for the driver's position, and unmounting — the trip finishing —
 * stops it. The camera stays on the trip rather than chasing the driver, so a
 * rider watching the car approach is not also watching the map lurch.
 */
const FollowedMap = (props: { readonly trip: Trip; readonly route: ReadonlyArray<MapPoint>; }) => {
  const position = useAtomValue(driverPositionAtom(props.trip.id));
  const driver: ReadonlyArray<MapMarker> = AsyncResult.isSuccess(position)
    ? [{
      id: "driver",
      position: { lat: position.value.lat, lng: position.value.lng },
      kind: "driver",
    }]
    : [];

  return (
    <SurgeMap
      markers={[...stops(props.trip), ...driver]}
      route={props.route}
      follow={[props.trip.pickup, props.trip.dropoff]}
    />
  );
};
