import { runtime } from "@/atom/runtime.js";
import { Realtime } from "@surge/client/Realtime";
import type { DriverStatus, Offer } from "@surge/domain/realtime/Wire";
import { Effect, Stream } from "effect";
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

/** Report a position. `Realtime` owns the epoch and sequence; nothing here does. */
export const reportPosition = runtime.fn(
  Effect.fnUntraced(function*(position: {
    readonly lat: number;
    readonly lng: number;
    readonly heading: number;
    readonly speedMps: number;
    readonly status: DriverStatus;
  }) {
    const realtime = yield* Realtime;
    yield* realtime.ping(position);
  }),
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
  Effect.fnUntraced(function*(answer: { readonly offer: Offer; readonly accepted: boolean; }) {
    const realtime = yield* Realtime;
    yield* realtime.reply(answer.offer, answer.accepted);
  }),
);
