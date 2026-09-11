import { ActionError } from "@/components/app/action-error.js";
import {
  type MapInset,
  type MapMarker,
  type MapPoint,
  SurgeMap,
} from "@/components/map/surge-map.js";
import { actionVariants, IconBubble, Sign } from "@/components/sign/sign.js";
import {
  type Stop,
  StopName,
  StopRow,
  StopsCard,
} from "@/routes/_protected/ride/-components/place-field.js";
import { RideLayout, useRideMapInset } from "@/routes/_protected/ride/-components/ride-layout.js";
import { HoldStep } from "@/routes/_protected/ride/-components/trip-payment.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { driverPositionAtom } from "@surge/common/atom/realtime-atoms";
import { activeTripAtom, cancelTrip } from "@surge/common/atom/trip-atoms";
import { formatCents, formatDistance, formatDuration, riderStatus } from "@surge/common/lib/format";
import { decodePolyline6 } from "@surge/domain/geo/Polyline";
import { isPending, isUnderway, type Trip } from "@surge/domain/trip/Trip";
import { Option, Result } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";
import { Car, Check, CreditCard, LoaderCircle } from "lucide-react";
import type * as React from "react";

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
  const inset = useRideMapInset();

  // The parent swaps this out when the trip finishes; this covers the render in
  // between rather than throwing.
  if (Option.isNone(active)) return null;
  const trip = active.value;

  const route = Result.getOrElse(decodePolyline6(trip.route.polyline6), () => []);
  // The state machine allows cancelling until the rider is in the car.
  const cancellable = isPending(trip.status) || trip.status === "TRIP_STATUS_ACCEPTED"
    || trip.status === "TRIP_STATUS_ARRIVED";
  const pickup: Stop = { position: trip.pickup, place: Option.none() };
  const dropoff: Stop = { position: trip.dropoff, place: Option.none() };

  return (
    <RideLayout
      title="Your trip"
      // Cancelling is the one action a waiting rider has, so it sits where
      // Go sat: under the thumb, until the rider is in the car.
      footer={cancellable
        ? (
          <button
            type="button"
            disabled={cancelling.waiting}
            onClick={() => {
              cancel({ tripId: trip.id, reason: "rider cancelled" });
            }}
            className={actionVariants({ tone: "danger", size: "block" })}
          >
            {cancelling.waiting ? "Cancelling…" : "Cancel trip"}
          </button>
        )
        : null}
      map={isUnderway(trip.status) && trip.driverId !== ""
        ? <FollowedMap trip={trip} route={route} inset={inset} />
        : (
          <SurgeMap
            markers={stops(trip)}
            route={route}
            follow={route.length > 1 ? route : [trip.pickup, trip.dropoff]}
            inset={inset}
          />
        )}
    >
      {
        /* Announced as it changes: this is the update a rider is waiting for.
          The card inside is keyed by status, so each change wipes a new one in. */
      }
      <output aria-live="polite" className="block">
        <StatusCard key={trip.status} trip={trip} pickup={pickup} dropoff={dropoff} />
      </output>

      {trip.status === "TRIP_STATUS_PAYMENT_PENDING" && (
        <HoldStep tripId={trip.id} totalCents={trip.totalCents} />
      )}

      <StopsCard
        rail
        footer={
          <p className="px-3 pt-0.5 pb-1.5 text-right text-sm font-semibold tabular-nums">
            {formatDuration(trip.route.seconds)} · {formatDistance(trip.route.meters)}
          </p>
        }
      >
        <StopRow kind="pickup" label="From" stop={pickup} />
        <StopRow kind="dropoff" label="To" stop={dropoff} />
      </StopsCard>

      <Sign tone="choice" className="items-center">
        <IconBubble>
          <CreditCard />
        </IconBubble>
        <span className="flex-1 text-base font-semibold">Fare</span>
        <span className="text-[22px] leading-none font-bold tabular-nums">
          {formatCents(trip.totalCents)}
        </span>
      </Sign>

      {AsyncResult.isFailure(cancelling) && <ActionError cause={cancelling.cause} />}

      <p className="px-1 pt-1 text-center text-xs text-muted-foreground">
        Trip <span className="font-mono">{trip.id}</span>
      </p>
    </RideLayout>
  );
};

/**
 * The card at the top of the sheet: what is happening now, in the colour that
 * says what kind of news it is — yellow while the trip is still moving towards
 * the rider, green when the car is there, black while the card is being asked.
 */
const StatusCard = (props: {
  readonly trip: Trip;
  readonly pickup: Stop;
  readonly dropoff: Stop;
}) => {
  const { status } = props.trip;
  const tone = status === "TRIP_STATUS_PAYMENT_PENDING"
    ? "service"
    : status === "TRIP_STATUS_ARRIVED"
    ? "done"
    : "direction";
  const glyph: React.ReactNode = status === "TRIP_STATUS_PAYMENT_PENDING"
    ? <CreditCard />
    : status === "TRIP_STATUS_REQUESTED" || status === "TRIP_STATUS_OFFERED"
    ? <LoaderCircle className="motion-safe:animate-spin" />
    : status === "TRIP_STATUS_ARRIVED"
    ? <Check />
    : <Car />;

  return (
    <Sign tone={tone} className="items-center py-4 motion-safe:animate-status-in">
      <IconBubble className={tone === "direction" ? undefined : "bg-white/15"}>{glyph}</IconBubble>
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <p className="text-xl leading-tight font-bold">{riderStatus[status]}</p>
        <p className="text-sm opacity-80">
          {status === "TRIP_STATUS_PAYMENT_PENDING"
            ? "The fare is held before a driver is asked."
            : status === "TRIP_STATUS_REQUESTED" || status === "TRIP_STATUS_OFFERED"
            ? "Offering your trip to drivers nearby."
            : status === "TRIP_STATUS_ACCEPTED"
            ? <DriverEta tripId={props.trip.id} />
            : status === "TRIP_STATUS_ARRIVED"
            ? (
              <>
                Meet them at <StopName stop={props.pickup} />.
              </>
            )
            : status === "TRIP_STATUS_IN_PROGRESS"
            ? (
              <>
                To <StopName stop={props.dropoff} />.
              </>
            )
            : null}
        </p>
      </div>
    </Sign>
  );
};

/**
 * The predicted wait while the driver is on the way, learned from pickups the
 * fleet has made; until there is one, where they are heading.
 */
const DriverEta = (props: { readonly tripId: Trip["id"]; }) => {
  const position = useAtomValue(driverPositionAtom(props.tripId));
  return AsyncResult.isSuccess(position) && position.value.etaSeconds > 0
    ? <>Arriving in about {formatDuration(position.value.etaSeconds)}.</>
    : <>Heading to your pickup.</>;
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
const FollowedMap = (props: {
  readonly trip: Trip;
  readonly route: ReadonlyArray<MapPoint>;
  readonly inset: MapInset;
}) => {
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
      follow={props.route.length > 1 ? props.route : [props.trip.pickup, props.trip.dropoff]}
      inset={props.inset}
    />
  );
};
