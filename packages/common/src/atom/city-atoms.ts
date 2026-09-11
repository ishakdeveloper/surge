import { Realtime } from "@surge/client/Realtime";
import type { CityCar, Viewport } from "@surge/domain/realtime/Wire";
import { Clock, Effect, Option, Stream } from "effect";
import { Atom } from "effect/unstable/reactivity";
import { runtime } from "./runtime.js";

/**
 * The city as a rider's map shows it: the cars free to take a trip nearby, and
 * a flash wherever somebody has just booked one.
 *
 * Any signed-in map may watch it, because the gateway shapes it to be safe to:
 * no driver ids, no car that is carrying or fetching somebody, and bookings as
 * the area they were made in rather than the address. Like the console's
 * fleet, the subscription follows the map — the viewport is state, and telling
 * the gateway about it is an atom that reruns whenever it moves.
 */

/** What the map is showing. Wrapped, because `Atom.make` reads an Option as an Effect. */
export interface CityView {
  readonly viewport: Option.Option<Viewport>;
}

export const cityViewAtom = Atom.make<CityView>({ viewport: Option.none() });

/**
 * Tells the gateway what the map is showing, again whenever it moves. Only
 * ever a watch: stopping belongs to `cityAtom`, whose lifetime is the map's,
 * for the reason `fleetWatchAtom` gives.
 */
export const cityWatchAtom = runtime.atom((get) =>
  Effect.gen(function*() {
    const { viewport } = get(cityViewAtom);
    if (Option.isNone(viewport)) return;
    const realtime = yield* Realtime;
    yield* realtime.watchCity(viewport);
  })
);

/** A booking, and when this client first heard of it — which is when its flash starts. */
export interface Flash {
  readonly key: string;
  readonly lat: number;
  readonly lng: number;
  readonly seenAtMs: number;
}

export interface City {
  readonly cars: ReadonlyArray<CityCar>;
  readonly flashes: ReadonlyArray<Flash>;
}

const EMPTY: City = { cars: [], flashes: [] };

/**
 * The latest cars, and the bookings still in the feed with when each was first
 * seen. A booking rides several frames; remembering its first sighting is what
 * makes it flash once rather than once a second.
 *
 * The unwatch is tied to this stream, so a map leaving the screen stops the
 * gateway sending the city to nobody.
 */
export const cityAtom = runtime.atom(
  Stream.unwrap(
    Effect.gen(function*() {
      const realtime = yield* Realtime;
      yield* Effect.addFinalizer(() => realtime.watchCity(Option.none()));
      return realtime.city.pipe(
        Stream.mapEffect((update) =>
          Effect.map(Clock.currentTimeMillis, (now) => ({ update, now }))
        ),
        Stream.scan(EMPTY, (previous, { update, now }): City => {
          const seen = new Map(previous.flashes.map((flash) => [flash.key, flash.seenAtMs]));
          return {
            cars: update.cars,
            flashes: update.bookings.map((booking) => ({
              key: booking.key,
              lat: booking.lat,
              lng: booking.lng,
              seenAtMs: seen.get(booking.key) ?? now,
            })),
          };
        }),
      );
    }),
  ),
);
