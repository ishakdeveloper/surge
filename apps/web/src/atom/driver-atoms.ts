import { runtime } from "@/atom/runtime.js";
import { activeTripAtom } from "@/atom/trip-atoms.js";
import { Geolocation } from "@surge/client/Geolocation";
import { Realtime } from "@surge/client/Realtime";
import type { LatLng } from "@surge/domain/geo/Polyline";
import type { DriverStatus } from "@surge/domain/realtime/Wire";
import type { Trip } from "@surge/domain/trip/Trip";
import { Effect, Option, Stream } from "effect";
import { AsyncResult, Atom } from "effect/unstable/reactivity";

/**
 * A driver's shift: whether they want work, and where they are.
 *
 * Per tab and deliberately not persisted. A driver who reloads the page is
 * asked again whether they are working, rather than silently put back in the
 * pool from wherever they were an hour ago.
 */
export interface Shift {
  readonly online: boolean;
  readonly position: Option.Option<LatLng>;
}

export const shiftAtom = Atom.make<Shift>({ online: false, position: Option.none() });

/**
 * What the matcher is told, derived — the driver never chooses it.
 *
 * The trip decides first. A driver on their way to a pickup who kept reporting
 * `idle` would be offered a second ride while delivering the first, so once a
 * trip is accepted the status follows the trip, whatever the toggle says.
 */
export const statusFor = (online: boolean, trip: Option.Option<Trip>): DriverStatus => {
  if (Option.isSome(trip)) {
    if (trip.value.status === "TRIP_STATUS_ACCEPTED") return "enroute_pickup";
    if (
      trip.value.status === "TRIP_STATUS_ARRIVED" || trip.value.status === "TRIP_STATUS_IN_PROGRESS"
    ) {
      return "on_trip";
    }
  }
  return online ? "idle" : "offline";
};

/**
 * The position, reported every four seconds while the page is mounted.
 *
 * Recomputed whenever the shift or the trip changes, which restarts the tick —
 * and `Stream.tick` emits at once, so moving the pin or going online is
 * reported immediately rather than up to four seconds later.
 *
 * Offline still reports, with status `offline`: the console shows supply, not
 * only availability, and a driver disappearing from the map because they
 * paused is a map that lies. Heading and speed are zero because a page with a
 * dropped pin has neither; the simulator is what exercises them.
 */
export const pingLoopAtom = runtime.atom((get) => {
  const shift = get(shiftAtom);
  if (Option.isNone(shift.position)) return Stream.empty;

  const position = shift.position.value;
  const status = statusFor(shift.online, Option.flatten(AsyncResult.value(get(activeTripAtom))));

  return Stream.unwrap(
    Effect.map(Realtime, (realtime) =>
      Stream.tick("4 seconds").pipe(
        Stream.mapEffect(() => realtime.ping({ ...position, heading: 0, speedMps: 0, status })),
      )),
  );
});

/** One fix from the device, placed as the driver's position. */
export const locateMe = runtime.fn(
  Effect.fnUntraced(function*(_: void, get: Atom.FnContext) {
    const geolocation = yield* Geolocation;
    const position = yield* geolocation.current;
    get.set(shiftAtom, { ...get(shiftAtom), position: Option.some(position) });
    return position;
  }),
);
