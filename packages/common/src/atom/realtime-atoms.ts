import { runtime } from "@/atom/runtime.js";
import { Realtime } from "@surge/client/Realtime";
import type { TripId } from "@surge/domain/api/Primitives";
import type { Offer } from "@surge/domain/realtime/Wire";
import { Effect, Option, Stream } from "effect";
import { Atom } from "effect/unstable/reactivity";

/**
 * The WebSocket, as atoms.
 *
 * Deliberately absent from `reactivity-keys.ts`: these are a subscription, not
 * a cache. Live movement arrives many times a second and routing it through
 * invalidation would turn a stream into a refetch storm.
 *
 * The connection itself belongs to `Realtime`, which holds it for the runtime's
 * lifetime, so mounting and unmounting a component subscribes and unsubscribes
 * rather than connecting and disconnecting.
 */

/** Whether the gateway has acknowledged us. What the reconnecting banner reads. */
export const connectionAtom = runtime.atom(
  Stream.unwrap(Effect.map(Realtime, (realtime) => realtime.status)),
);

/**
 * Offers, newest last, one per trip.
 *
 * Accumulated here rather than in a component because an offer is a fact that
 * arrived at a moment: React state initialised from "the latest value" would
 * lose every offer that landed while the driver was on another route, and a
 * driver who missed an offer because they opened a menu is a driver who thinks
 * the app is broken.
 *
 * Re-keyed by trip id on the way in: the matcher re-dispatches an expired offer
 * to the same driver, and the second one supersedes the first rather than
 * appearing beside it.
 */
export const offersAtom = runtime.atom(
  Stream.unwrap(
    Effect.map(Realtime, (realtime) =>
      realtime.offers.pipe(
        Stream.scan(
          [] as ReadonlyArray<Offer>,
          (open, offer) => [...open.filter((existing) => existing.tripId !== offer.tripId), offer],
        ),
      )),
  ),
);

/**
 * A one-second tick, so an offer's countdown is a rendered value rather than a
 * `setInterval` in a component.
 *
 * An atom rather than a hook because several components read the same clock and
 * they should agree: a card that says four seconds beside a bar that says three
 * is a bug people notice.
 */
export const nowAtom = Atom.make(
  Stream.tick("1 second").pipe(Stream.map(() => Date.now())),
  { initialValue: Date.now() },
);

/**
 * Answer an offer.
 *
 * Takes the offer rather than its id so the reply carries the cell it arrived
 * on — the shard holding the reservation is the only one that can resolve it,
 * and the driver has very likely moved since.
 *
 * No `reactivityKeys`: the answer's consequence arrives back over the socket as
 * a trip the driver is now on, not as a cache that needs refreshing.
 */
export const answerOffer = runtime.fn(
  Effect.fnUntraced(
    function*(answer: { readonly offer: Offer; readonly accepted: boolean; }, get: Atom.FnContext) {
      const realtime = yield* Realtime;
      yield* realtime.reply(answer.offer, answer.accepted);
      get.set(answeredAtom, {
        tripIds: new Set([...get(answeredAtom).tripIds, answer.offer.tripId]),
      });
    },
  ),
);

/**
 * Offers already answered, by trip id, so they leave the list at once rather
 * than when they expire. `offersAtom` is a record of what arrived; this is what
 * the driver did about it, kept apart so neither has to be rewritten.
 */
export interface Answered {
  readonly tripIds: ReadonlySet<Offer["tripId"]>;
}

// Wrapped in an object on purpose: `Atom.make` given a bare Set — anything
// iterable — resolves to its Effect overload, since an Effect is iterable too.
export const answeredAtom = Atom.make<Answered>({ tripIds: new Set<Offer["tripId"]>() });

/**
 * Where the rider's driver is, once a second, for as long as it is read.
 *
 * Following is a request the gateway checks against the trip service as the
 * rider — the same rule that decides who may read a trip decides who may watch
 * its driver — and it lasts as long as this atom does: the finalizer unfollows
 * when the trip leaves the screen.
 */
export const driverPositionAtom = Atom.family((tripId: TripId) =>
  runtime.atom(
    Stream.unwrap(
      Effect.gen(function*() {
        const realtime = yield* Realtime;
        yield* realtime.followTrip(Option.some(tripId));
        yield* Effect.addFinalizer(() => realtime.followTrip(Option.none()));
        return realtime.positions.pipe(Stream.filter((position) => position.tripId === tripId));
      }),
    ),
  )
);
