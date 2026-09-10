import { Keys } from "@/atom/reactivity-keys.js";
import { runtime } from "@/atom/runtime.js";
import { stripe, StripeRefused } from "@/lib/stripe.js";
import type { Stripe, StripeElements } from "@stripe/stripe-js";
import { Realtime } from "@surge/client/Realtime";
import { SurgeApi } from "@surge/client/SurgeApi";
import { Cents, type TripId } from "@surge/domain/api/Primitives";
import { Effect, Option, Stream } from "effect";
import { Atom, Reactivity } from "effect/unstable/reactivity";

/**
 * Money, over the generated REST surface, kept fresh by the WebSocket — the
 * same arrangement as `trip-atoms.ts`. The payments service pushes
 * `PaymentsChanged` to whoever a change concerns, and `paymentPushesAtom`
 * turns that into an invalidation of `Keys.payments`.
 *
 * Two kinds of write live here. Ours go to the gateway. Stripe's go from the
 * browser straight to Stripe — saving a card, confirming a hold with the bank —
 * and their result reaches our servers afterwards, by webhook. So a Stripe
 * write succeeding says Stripe has it; the page learns it is recorded when the
 * push arrives.
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

/**
 * Submit the card form to Stripe.
 *
 * One per SetupIntent, so replacing a card starts from a clean form rather than
 * from the last one's success. `redirect: "if_required"` never redirects here:
 * the SetupIntent is created with `allow_redirects: never`, because a hold is
 * later placed on this card with nobody at the keyboard.
 */
export const confirmCardAtom = Atom.family((_clientSecret: string) =>
  runtime.fn(
    Effect.fnUntraced(
      function*(form: { readonly stripe: Stripe; readonly elements: StripeElements; }) {
        const result = yield* Effect.tryPromise({
          try: () => form.stripe.confirmSetup({ elements: form.elements, redirect: "if_required" }),
          catch: () => unavailable,
        });
        if (result.error !== undefined) return yield* StripeRefused.fromStripe(result.error);
        return result.setupIntent;
      },
    ),
    { reactivityKeys: [Keys.payments] },
  )
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

/**
 * Run the bank's check on a hold — 3-D Secure, usually — in Stripe's own modal.
 *
 * The rider starts it with a click rather than it opening by itself: it is a
 * context switch to their bank, and one they should see coming.
 */
export const confirmHoldAtom = Atom.family((_tripId: TripId) =>
  runtime.fn(
    Effect.fnUntraced(function*(clientSecret: string) {
      const instance = yield* stripeJs;
      const result = yield* Effect.tryPromise({
        try: () => instance.handleNextAction({ clientSecret }),
        catch: () => unavailable,
      });
      if (result.error !== undefined) return yield* StripeRefused.fromStripe(result.error);
    }),
    { reactivityKeys: [Keys.payments] },
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

/** How many rows each table asks for. The page says so when there are more. */
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
 * Send the driver to Stripe's onboarding.
 *
 * The link is followed inside the same Effect that fetched it, so it never
 * lands in React state: it is single use, short lived, and opens the driver's
 * identity details to whoever holds it.
 */
export const openOnboarding = runtime.fn(
  Effect.fnUntraced(function*() {
    const api = yield* SurgeApi;
    const { url } = yield* api.payments.startOnboarding({ payload: {} });
    yield* Effect.sync(() => {
      window.location.assign(url);
    });
  }),
);

/** Into the driver's Stripe Express dashboard, by a single-use login link. */
export const openDashboard = runtime.fn(
  Effect.fnUntraced(function*() {
    const api = yield* SurgeApi;
    const { url } = yield* api.payments.createDashboardLink({ payload: {} });
    yield* Effect.sync(() => {
      window.location.assign(url);
    });
  }),
);

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
 * available — what the page asks for, since the balance it shows may be a
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
 * Pushes, turned into invalidation. Mounted by every page that shows money,
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

// --- Stripe.js ---------------------------------------------------------------

const unavailable = new StripeRefused({
  code: "stripe_unavailable",
  detail: "Stripe could not be reached. Check your connection and try again.",
});

/** The page's Stripe.js, or why there is none. */
const stripeJs = Effect.gen(function*() {
  const loading = yield* Option.match(stripe, {
    onNone: () =>
      Effect.fail(
        new StripeRefused({
          code: "stripe_not_configured",
          detail: "This app has no Stripe publishable key, so it cannot run your bank's check.",
        }),
      ),
    onSome: Effect.succeed,
  });
  const loaded = yield* Effect.tryPromise({ try: () => loading, catch: () => unavailable });
  if (loaded === null) return yield* unavailable;
  return loaded;
});
