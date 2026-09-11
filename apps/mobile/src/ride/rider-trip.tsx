import { Announced } from "@/components/app/announced.js";
import { Detail, Details } from "@/components/app/details.js";
import { ActionError } from "@/components/app/errors.js";
import { Screen } from "@/components/app/screen.js";
import { type MapMarker, type MapPoint, SurgeMap } from "@/components/map/surge-map.js";
import { Button } from "@/components/ui/button.js";
import { Text } from "@/components/ui/text.js";
import { HoldStep } from "@/ride/trip-payment.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { driverPositionAtom } from "@surge/common/atom/realtime-atoms";
import { activeTripAtom, cancelTrip } from "@surge/common/atom/trip-atoms";
import {
  formatCents,
  formatDistance,
  formatDuration,
  formatPoint,
  riderStatus,
} from "@surge/common/lib/format";
import { decodePolyline6 } from "@surge/domain/geo/Polyline";
import { isPending, isUnderway, type Trip } from "@surge/domain/trip/Trip";
import { Option, Result } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";

/**
 * A trip in flight, from the rider's side. Reads the active trip itself, per
 * `knowledge/rules/effect-atom.md`: the screen has guarded loading and
 * failure, and a push moving the trip re-renders this on its own.
 */
export const RiderTrip = () => {
  const active = useAtomValue(activeTripAtom).pipe(AsyncResult.getOrThrow);
  const cancelling = useAtomValue(cancelTrip);
  const cancel = useAtomSet(cancelTrip);

  // The screen swaps this out when the trip finishes; this covers the render
  // in between rather than throwing.
  if (Option.isNone(active)) return null;
  const trip = active.value;
  const route = Result.getOrElse(decodePolyline6(trip.route.polyline6), () => []);

  // The state machine allows cancelling until the rider is in the car.
  const cancellable = isPending(trip.status) || trip.status === "TRIP_STATUS_ACCEPTED"
    || trip.status === "TRIP_STATUS_ARRIVED";

  return (
    <Screen>
      {/* Announced as it changes: this is the update a rider is waiting for. */}
      <Announced
        message={riderStatus[trip.status]}
        className="gap-1 rounded-xl border border-border bg-card p-4"
      >
        <Text className="text-base font-medium">{riderStatus[trip.status]}</Text>
        <Text className="font-mono text-xs text-muted-foreground">{trip.status}</Text>
      </Announced>

      {isUnderway(trip.status) && trip.driverId !== ""
        ? <FollowedMap trip={trip} route={route} />
        : <SurgeMap markers={stops(trip)} route={route} follow={[trip.pickup, trip.dropoff]} />}

      {trip.status === "TRIP_STATUS_PAYMENT_PENDING" && (
        <HoldStep tripId={trip.id} totalCents={trip.totalCents} />
      )}
      {isUnderway(trip.status) && trip.driverId !== "" && <DriverEta tripId={trip.id} />}

      <Details>
        <Detail label="Trip" value={trip.id} mono />
        <Detail label="Driver" value={trip.driverId === "" ? "-" : trip.driverId} mono />
        <Detail label="Pickup" value={formatPoint(trip.pickup)} mono />
        <Detail label="Dropoff" value={formatPoint(trip.dropoff)} mono />
        <Detail label="Fare" value={formatCents(trip.totalCents)} />
        <Detail
          label="Route"
          value={`${formatDistance(trip.route.meters)} · about ${
            formatDuration(trip.route.seconds)
          }`}
        />
      </Details>

      {AsyncResult.isFailure(cancelling) && <ActionError cause={cancelling.cause} />}
      {cancellable && (
        <Button
          variant="outline"
          disabled={cancelling.waiting}
          onPress={() => {
            cancel({ tripId: trip.id, reason: "rider cancelled" });
          }}
        >
          {cancelling.waiting ? "Cancelling…" : "Cancel trip"}
        </Button>
      )}
    </Screen>
  );
};

/**
 * The predicted wait while the driver is on the way. Its own component because
 * following is a subscription: mounting asks the gateway for the driver's
 * position, and unmounting stops it.
 */
const DriverEta = (props: { readonly tripId: Trip["id"]; }) => {
  const position = useAtomValue(driverPositionAtom(props.tripId));
  if (!AsyncResult.isSuccess(position) || position.value.etaSeconds <= 0) return null;
  return <Text>Arriving in about {formatDuration(position.value.etaSeconds)}</Text>;
};

const stops = (trip: Trip): ReadonlyArray<MapMarker> => [
  { id: "pickup", position: trip.pickup, kind: "pickup" },
  { id: "dropoff", position: trip.dropoff, kind: "dropoff" },
];

/**
 * The map once a driver is assigned, with the driver on it — the web's
 * `FollowedMap`. The camera stays on the trip rather than chasing the car, so
 * a rider watching it approach is not also watching the map lurch.
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
