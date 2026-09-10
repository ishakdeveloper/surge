import { Keys } from "@/atom/reactivity-keys.js";
import { runtime } from "@/atom/runtime.js";
import { SurgeApi } from "@surge/client/SurgeApi";
import { type Coordinate, type FareId, isFinished, type TripId } from "@surge/domain/trip/Trip";
import { Effect, Schedule, Stream } from "effect";
import { Atom } from "effect/unstable/reactivity";

/**
 * Trips, over the generated REST surface.
 *
 * Reads subscribe with `Atom.withReactivity`; writes announce with
 * `reactivityKeys`, so booking a trip refreshes the list without either side
 * knowing the other exists.
 */

/**
 * A fare quote for a pickup and a dropoff.
 *
 * A write rather than a read despite reading nothing: it is a POST with a body,
 * the quotes it returns are priced at a moment, and a rider expects the button
 * they pressed to be what produced them. Caching it by coordinates would show
 * yesterday's surge.
 */
export const previewTrip = runtime.fn(
  Effect.fnUntraced(function*(route: {
    readonly pickup: Coordinate;
    readonly dropoff: Coordinate;
  }) {
    const api = yield* SurgeApi;
    return yield* api.trips.preview({ payload: route });
  }),
);

/**
 * Book a fare.
 *
 * The caller supplies the idempotency key and must keep it stable across
 * retries — that is the entire mechanism. A key generated inside this function
 * would be a new key on every attempt, which is a second ride rather than a
 * retry, and the trip service would be correct to give them one.
 */
export const bookTrip = runtime.fn(
  Effect.fnUntraced(function*(booking: {
    readonly fareId: FareId;
    readonly idempotencyKey: string;
  }) {
    const api = yield* SurgeApi;
    return yield* api.trips.create({
      payload: { fareId: booking.fareId },
      headers: { "idempotency-key": booking.idempotencyKey },
    });
  }),
  { reactivityKeys: [Keys.trips] },
);

export const cancelTrip = runtime.fn(
  Effect.fnUntraced(function*(cancellation: {
    readonly tripId: TripId;
    readonly reason: string;
  }) {
    const api = yield* SurgeApi;
    return yield* api.trips.cancel({
      params: { tripId: cancellation.tripId },
      payload: { reason: cancellation.reason },
    });
  }),
  { reactivityKeys: [Keys.trips] },
);

/** The caller's own trips. There is no parameter for whose — the token decides. */
export const tripsAtom = Atom.withReactivity([Keys.trips])(
  runtime.atom(
    Effect.gen(function*() {
      const api = yield* SurgeApi;
      const listed = yield* api.trips.list({ query: {} });
      return listed.trips;
    }),
  ),
);

/**
 * One trip, followed until it stops moving.
 *
 * Polled, and this is the part of the system that should not stay polled. Every
 * other piece of live state here arrives over the WebSocket; a trip's status
 * does not, because nothing yet produces rider-addressed messages to `ws.push`
 * — the matcher only publishes offers, which are addressed to drivers. Two
 * seconds for the few seconds a match takes is off the hot path and costs
 * nothing measurable, but it is the wrong shape for this system and is written
 * here rather than hidden in a component so that it is easy to delete.
 *
 * The stream stops on its own once the trip reaches a terminal state, so a
 * completed trip is not polled forever by a tab somebody left open.
 */
export const tripAtom = Atom.family((tripId: TripId) =>
  Atom.withReactivity([Keys.trips])(
    runtime.atom(
      Stream.unwrap(
        Effect.map(SurgeApi, (api) =>
          Stream.fromEffectRepeat(api.trips.get({ params: { tripId } })).pipe(
            Stream.schedule(Schedule.spaced("2 seconds")),
            Stream.map((response) =>
              response.trip
            ),
            Stream.takeUntil((trip) => isFinished(trip.status)),
          )),
      ),
    ),
  )
);
