import { Geolocation } from "@surge/client/Geolocation";
import { Realtime } from "@surge/client/Realtime";
import type { LatLng } from "@surge/domain/geo/Polyline";
import { Effect, Option, Stream } from "effect";
import { AsyncResult, Atom } from "effect/unstable/reactivity";
import { statusFor } from "../drive/driver-status.js";
import { runtime } from "./runtime.js";
import { activeTripAtom } from "./trip-atoms.js";

/**
 * A driver's shift: whether they want work, and where they are.
 *
 * Per registry and deliberately not persisted. A driver who reloads the page
 * or reopens the app is asked again whether they are working, rather than
 * silently put back in the pool from wherever they were an hour ago.
 */
export interface Shift {
  readonly online: boolean;
  readonly position: Option.Option<LatLng>;
}

export const shiftAtom = Atom.make<Shift>({ online: false, position: Option.none() });

/**
 * Whether the shift's position follows the device, rather than staying where
 * it was placed. A separate atom from the shift so that a new fix — which
 * rewrites the shift — does not restart the thing that produced it.
 */
export const followingAtom = Atom.make(false);

/**
 * The device's own fixes, written into the shift while `followingAtom` is on.
 * Mounted by the driver's screen beside the ping loop, which then reports
 * wherever the driver actually is.
 *
 * Its value is the latest fix, or why there is none — a refused permission is
 * shown where the toggle is, not swallowed.
 *
 * Not following is a stream that never emits rather than an empty one: an atom
 * over a stream that ends with nothing in it fails with `NoSuchElementError`,
 * which the toggle would show as a tracking error nobody asked for.
 */
export const followAtom = runtime.atom((get) => {
  if (!get(followingAtom)) return Stream.never;

  return Stream.unwrap(Effect.map(Geolocation, (geolocation) => geolocation.watch)).pipe(
    Stream.tap((position) =>
      Effect.sync(() => {
        get.set(shiftAtom, { ...get.once(shiftAtom), position: Option.some(position) });
      })
    ),
  );
});

/**
 * The position, reported every four seconds while the driver's screen is mounted.
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
  const trip = Option.flatten(AsyncResult.value(get(activeTripAtom)));
  const status = statusFor(shift.online, Option.map(trip, (current) => current.status));

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
