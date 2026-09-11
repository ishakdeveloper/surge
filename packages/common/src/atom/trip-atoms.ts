import { Keys } from "@/atom/reactivity-keys.js";
import { runtime } from "@/atom/runtime.js";
import { Realtime } from "@surge/client/Realtime";
import { SurgeApi } from "@surge/client/SurgeApi";
import type { FareId, TripId } from "@surge/domain/api/Primitives";
import { type Coordinate, isPending, isUnderway, type Trip } from "@surge/domain/trip/Trip";
import { Effect, Option, Stream } from "effect";
import { AsyncResult, Atom, Reactivity } from "effect/unstable/reactivity";

/**
 * Trips, over the generated REST surface, kept fresh by the WebSocket.
 *
 * Reads subscribe with `Atom.withReactivity`; writes announce with
 * `reactivityKeys`. What makes it live is `tripPushesAtom` below: the trip
 * service pushes every change to the people on the trip, and that one atom
 * turns each push into an invalidation, so every trip query reruns the moment
 * the server knows something new — and none of them polls.
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
    return yield* api.trips.create({ payload: booking });
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

/**
 * The driver's three steps. The trip service refuses them from anyone but the
 * assigned driver, and refuses them out of order, so the page only has to offer
 * the one that comes next.
 */
export const arriveTrip = runtime.fn(
  Effect.fnUntraced(function*(tripId: TripId) {
    const api = yield* SurgeApi;
    return yield* api.trips.arrive({ params: { tripId } });
  }),
  { reactivityKeys: [Keys.trips] },
);

export const startTrip = runtime.fn(
  Effect.fnUntraced(function*(tripId: TripId) {
    const api = yield* SurgeApi;
    return yield* api.trips.start({ params: { tripId } });
  }),
  { reactivityKeys: [Keys.trips] },
);

export const completeTrip = runtime.fn(
  Effect.fnUntraced(function*(tripId: TripId) {
    const api = yield* SurgeApi;
    return yield* api.trips.complete({ params: { tripId } });
  }),
  { reactivityKeys: [Keys.trips] },
);

/**
 * The caller's own trips, newest first. There is no parameter for whose — the
 * token decides, and it decides which side of a trip too: a rider sees the
 * trips they booked, a driver the ones they were assigned.
 */
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
 * The trip the caller is on right now, if any: still waiting for a driver, or
 * with one on the way or aboard.
 *
 * Derived rather than fetched, so there is one list of trips and this is a view
 * of it — a second request for "the current one" would be a second answer that
 * could disagree with the first.
 */
export const activeTripAtom = Atom.readable((get) =>
  AsyncResult.map(
    get(tripsAtom),
    (trips): Option.Option<Trip> =>
      Option.fromNullishOr(trips.find((trip) => isPending(trip.status) || isUnderway(trip.status))),
  )
);

/** One trip. Refreshed by pushes like every other trip query. */
export const tripAtom = Atom.family((tripId: TripId) =>
  Atom.withReactivity([Keys.trips])(
    runtime.atom(
      Effect.gen(function*() {
        const api = yield* SurgeApi;
        const { trip } = yield* api.trips.get({ params: { tripId } });
        return trip;
      }),
    ),
  )
);

/**
 * Pushes, turned into invalidation.
 *
 * Mounted once by each page that shows trips. Two things invalidate: a
 * `TripUpdated` push, which says a trip moved, and a *re*connect, which says
 * one might have — a push sent while the socket was down is gone, because the
 * gateway keeps no replay, so coming back is treated as having missed
 * something. The first connection is exempt: nothing can have been missed
 * before there was anything to miss.
 */
export const tripPushesAtom = runtime.atom(
  Stream.unwrap(
    Effect.map(Realtime, (realtime) =>
      Stream.merge(
        realtime.tripUpdates,
        realtime.status.pipe(
          Stream.changes,
          Stream.filter((status) => status === "Connected"),
          Stream.drop(1),
        ),
      ).pipe(Stream.mapEffect(() => Reactivity.invalidate([Keys.trips])))),
  ),
);
