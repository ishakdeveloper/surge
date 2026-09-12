import { Fleet, type Handover } from "@surge/client/Fleet";
import { SurgeApi } from "@surge/client/SurgeApi";
import type { DocumentId, UserId, VehicleId } from "@surge/domain/api/Primitives";
import type { DocumentKind } from "@surge/domain/fleet/Fleet";
import { Data, Effect } from "effect";
import { Atom } from "effect/unstable/reactivity";
import { Keys } from "./reactivity-keys.js";
import { runtime } from "./runtime.js";

/**
 * Driver vetting, as atoms.
 *
 * Everything here is a query under `Keys.fleet` and a mutation that
 * invalidates it, which is enough because none of it moves on its own: a
 * verification comes back through a webhook, and a reviewer's decision is a
 * person at a keyboard. What does move — the standing that follows — is read
 * again the next time the screen asks, and there is nothing a driver can do
 * about it in the second it takes.
 */

// --- the driver's own -----------------------------------------------------------

/** The caller's standing, their cars and their papers, in one read. */
export const myDriverAtom = Atom.withReactivity([Keys.fleet])(
  runtime.atom(
    Effect.gen(function*() {
      const api = yield* SurgeApi;
      return yield* api.fleet.getMyDriver();
    }),
  ),
);

/**
 * Open an identity check, and hand back the link to finish it at.
 *
 * Single use and short lived: the caller opens it at once and keeps it out of
 * state, as with Stripe's onboarding links.
 */
export const startIdentityCheck = runtime.fn(
  Effect.fnUntraced(function*(request: { readonly returnUrl: string; }) {
    const api = yield* SurgeApi;
    return yield* api.fleet.startIdentityCheck({ payload: request });
  }),
  { reactivityKeys: [Keys.fleet] },
);

/**
 * Register a car by its plate.
 *
 * Six characters is the whole form: the vehicle register answers with the
 * make, the model, the colour, the seats, the inspection date and whether
 * anybody insures it.
 */
export const addVehicle = runtime.fn(
  Effect.fnUntraced(function*(car: { readonly plate: string; readonly packageSlug: string; }) {
    const api = yield* SurgeApi;
    const { vehicle } = yield* api.fleet.addVehicle({ payload: car });
    return vehicle;
  }),
  { reactivityKeys: [Keys.fleet] },
);

export const retireVehicle = runtime.fn(
  Effect.fnUntraced(function*(vehicleId: VehicleId) {
    const api = yield* SurgeApi;
    const { vehicle } = yield* api.fleet.retireVehicle({ params: { vehicleId }, payload: {} });
    return vehicle;
  }),
  { reactivityKeys: [Keys.fleet] },
);

/**
 * Hand over a document: a link, the bytes, and the word that they arrived.
 *
 * The bytes come from each app's own picker — a file input in the browser, the
 * photo library on a phone — because that part cannot be shared. Everything
 * after it can.
 */
export const handDocument = runtime.fn(
  Effect.fnUntraced(function*(handover: Handover) {
    const fleet = yield* Fleet;
    return yield* fleet.hand(handover);
  }),
  { reactivityKeys: [Keys.fleet] },
);

/**
 * Say a VOG or a chauffeurskaart has been applied for.
 *
 * Neither has an API anywhere: Justis answers in weeks and Kiwa posts a card.
 * Recording the application is what lets the driver see what is being waited
 * on instead of wondering why they are still not approved.
 */
export const declareAuthorityDocument = runtime.fn(
  Effect.fnUntraced(function*(kind: DocumentKind) {
    const api = yield* SurgeApi;
    const { document } = yield* api.fleet.declareAuthorityDocument({ payload: { kind } });
    return document;
  }),
  { reactivityKeys: [Keys.fleet] },
);

// --- the reviewer's -------------------------------------------------------------

/** Who is waiting, longest first. Ops only; anyone else is refused by the server. */
export const reviewQueueAtom = Atom.withReactivity([Keys.fleet])(
  runtime.atom(
    Effect.gen(function*() {
      const api = yield* SurgeApi;
      const { items } = yield* api.review.list({ query: {} });
      return items;
    }),
  ),
);

/** One driver's whole case, with a link to each document. */
export const reviewItemAtom = Atom.family((driverId: UserId) =>
  Atom.withReactivity([Keys.fleet])(
    runtime.atom(
      Effect.gen(function*() {
        const api = yield* SurgeApi;
        return yield* api.review.get({ params: { driverId } });
      }),
    ),
  )
);

/** A reviewer's answer about one document: the expiry they read, or why not. */
export class Verdict extends Data.Class<{
  readonly documentId: DocumentId;
  readonly approve: boolean;
  /** As YYYY-MM-DD, read off the document. Required when approving one that expires. */
  readonly expiresAt: string;
  readonly note: string;
}> {
  static make(args: {
    readonly documentId: DocumentId;
    readonly approve: boolean;
    readonly expiresAt?: string;
    readonly note?: string;
  }) {
    return new Verdict({
      documentId: args.documentId,
      approve: args.approve,
      expiresAt: args.expiresAt ?? "",
      note: args.note ?? "",
    });
  }
}

export const decideDocument = runtime.fn(
  Effect.fnUntraced(function*(verdict: Verdict) {
    const api = yield* SurgeApi;
    return yield* api.review.decideDocument({
      params: { documentId: verdict.documentId },
      payload: { approve: verdict.approve, expiresAt: verdict.expiresAt, note: verdict.note },
    });
  }),
  { reactivityKeys: [Keys.fleet] },
);

export const decideVehicle = runtime.fn(
  Effect.fnUntraced(function*(decision: {
    readonly vehicleId: VehicleId;
    readonly approve: boolean;
    readonly note: string;
  }) {
    const api = yield* SurgeApi;
    return yield* api.review.decideVehicle({
      params: { vehicleId: decision.vehicleId },
      payload: { approve: decision.approve, note: decision.note },
    });
  }),
  { reactivityKeys: [Keys.fleet] },
);

/** Stop a driver, or lift it: an empty reason returns them to what their papers say. */
export const blockDriver = runtime.fn(
  Effect.fnUntraced(function*(block: { readonly driverId: UserId; readonly reason: string; }) {
    const api = yield* SurgeApi;
    const { driver } = yield* api.review.block({
      params: { driverId: block.driverId },
      payload: { reason: block.reason },
    });
    return driver;
  }),
  { reactivityKeys: [Keys.fleet] },
);
