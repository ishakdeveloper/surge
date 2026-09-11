import { Realtime } from "@surge/client/Realtime";
import { SurgeApi } from "@surge/client/SurgeApi";
import { Cents, type TripId } from "@surge/domain/api/Primitives";
import { Effect, Stream } from "effect";
import { Atom, Reactivity } from "effect/unstable/reactivity";
import { Keys } from "./reactivity-keys.js";
import { runtime } from "./runtime.js";

/**
 * Money, over the generated REST surface, kept fresh by the WebSocket — the
 * same arrangement as `trip-atoms.ts`. The payments service pushes
 * `PaymentsChanged` to whoever a change concerns, and `paymentPushesAtom`
 * turns that into an invalidation of `Keys.payments`.
 *
 * Only our own calls live here. Stripe's half — collecting a card, the bank's
 * 3-D Secure check, following an onboarding link — is a different SDK on each
 * platform, Stripe.js in the browser and Stripe's React Native SDK on the
 * phone, so each app keeps it beside its own UI. Their results reach our
 * servers by webhook either way, and come back to both apps as a push.
 */

// --- rider -------------------------------------------------------------------

/** The card holds are placed on. `saved` is false until there is one. */
export const cardAtom = Atom.withReactivity([Keys.payments])(
  runtime.atom(
    Effect.gen(function*() {
      const api = yield* SurgeApi;
      return yield* api.payments.getMethod();
    }),
  ),
);

/**
 * Begin saving a card.
 *
 * Against Stripe this returns the SetupIntent's client secret, which the card
 * form is mounted with. Against the fake processor there is no form: the fake
 * attaches its test Visa on the spot, so success here is the card saved.
 */
export const startCardSetup = runtime.fn(
  Effect.fnUntraced(function*() {
    const api = yield* SurgeApi;
    return yield* api.payments.createSetupIntent({ payload: {} });
  }),
  { reactivityKeys: [Keys.payments] },
);

/** What became of one trip's hold. `NotFound` until the payments service has seen the trip. */
export const tripPaymentAtom = Atom.family((tripId: TripId) =>
  Atom.withReactivity([Keys.payments])(
    runtime.atom(
      Effect.gen(function*() {
        const api = yield* SurgeApi;
        const { payment } = yield* api.payments.getForTrip({ params: { tripId } });
        return payment;
      }),
    ),
  )
);

// --- driver ------------------------------------------------------------------

/** Whether the driver can be paid yet, and whether Stripe is waiting on them. */
export const payoutAccountAtom = Atom.withReactivity([Keys.payments])(
  runtime.atom(
    Effect.gen(function*() {
      const api = yield* SurgeApi;
      const { account } = yield* api.payments.getPayoutAccount();
      return account;
    }),
  ),
);

/** Available, pending and owed, read from Stripe by the payments service. */
export const balanceAtom = Atom.withReactivity([Keys.payments])(
  runtime.atom(
    Effect.gen(function*() {
      const api = yield* SurgeApi;
      const { balance } = yield* api.payments.getBalance();
      return balance;
    }),
  ),
);

/** How many rows each list asks for. The screen says so when there are more. */
export const PAGE_SIZE = 50;

/** The driver's share of each trip, newest first. */
export const earningsAtom = Atom.withReactivity([Keys.payments])(
  runtime.atom(
    Effect.gen(function*() {
      const api = yield* SurgeApi;
      return yield* api.payments.listEarnings({ query: { pageSize: PAGE_SIZE } });
    }),
  ),
);

export const withdrawalsAtom = Atom.withReactivity([Keys.payments])(
  runtime.atom(
    Effect.gen(function*() {
      const api = yield* SurgeApi;
      return yield* api.payments.listWithdrawals({ query: { pageSize: PAGE_SIZE } });
    }),
  ),
);

/**
 * A single-use link into Stripe's hosted onboarding. The caller follows it at
 * once and keeps it out of state: it is short lived, and opens the driver's
 * identity details to whoever holds it.
 */
export const onboardingLink = Effect.gen(function*() {
  const api = yield* SurgeApi;
  const { url } = yield* api.payments.startOnboarding({ payload: {} });
  return url;
});

/** A single-use login link into the driver's Stripe Express dashboard. */
export const dashboardLink = Effect.gen(function*() {
  const api = yield* SurgeApi;
  const { url } = yield* api.payments.createDashboardLink({ payload: {} });
  return url;
});

/** A session for Stripe's embedded Connect components. Connect.js asks for one when it needs one. */
export const createAccountSession = runtime.fn(
  Effect.fnUntraced(function*() {
    const api = yield* SurgeApi;
    const { clientSecret } = yield* api.payments.createAccountSession({ payload: {} });
    return clientSecret;
  }),
);

/**
 * Pay the balance out to the driver's bank. An amount of zero is everything
 * available — what the screen asks for, since the balance it shows may be a
 * moment old.
 *
 * The caller holds the idempotency key across retries, as with booking: a key
 * made in here would be a second payout on every retry.
 */
export const withdraw = runtime.fn(
  Effect.fnUntraced(function*(request: { readonly idempotencyKey: string; }) {
    const api = yield* SurgeApi;
    return yield* api.payments.withdraw({
      payload: { amountCents: Cents.make(0), idempotencyKey: request.idempotencyKey },
    });
  }),
  { reactivityKeys: [Keys.payments] },
);

// --- pushes ------------------------------------------------------------------

/**
 * Pushes, turned into invalidation. Mounted by every screen that shows money,
 * and for the same two reasons as `tripPushesAtom`: a `PaymentsChanged` push,
 * and a reconnect, after which a push sent while the socket was down is gone.
 */
export const paymentPushesAtom = runtime.atom(
  Stream.unwrap(
    Effect.map(Realtime, (realtime) =>
      Stream.merge(
        realtime.paymentsChanged,
        realtime.status.pipe(
          Stream.changes,
          Stream.filter((status) => status === "Connected"),
          Stream.drop(1),
        ),
      ).pipe(Stream.mapEffect(() => Reactivity.invalidate([Keys.payments])))),
  ),
);
