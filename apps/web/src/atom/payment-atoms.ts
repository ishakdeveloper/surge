import { refusedBy, stripe } from "@/lib/stripe.js";
import type { Stripe, StripeElements } from "@stripe/stripe-js";
import { dashboardLink, onboardingLink } from "@surge/common/atom/payment-atoms";
import { Keys } from "@surge/common/atom/reactivity-keys";
import { runtime } from "@surge/common/atom/runtime";
import { StripeRefused } from "@surge/common/payments/stripe-refused";
import type { TripId } from "@surge/domain/api/Primitives";
import { Effect, Option } from "effect";
import { Atom } from "effect/unstable/reactivity";

/**
 * The browser's half of payments: Stripe.js, and following Stripe's links.
 *
 * Everything that talks to our own services is shared, in
 * `@surge/common/atom/payment-atoms`. What is here writes straight to Stripe
 * from the browser — saving a card, confirming a hold with the bank — and its
 * result reaches our servers afterwards, by webhook. So a Stripe write
 * succeeding says Stripe has it; the page learns it is recorded when the push
 * arrives.
 */

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
        if (result.error !== undefined) return yield* refusedBy(result.error);
        return result.setupIntent;
      },
    ),
    { reactivityKeys: [Keys.payments] },
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
      if (result.error !== undefined) return yield* refusedBy(result.error);
    }),
    { reactivityKeys: [Keys.payments] },
  )
);

/**
 * Send the driver to Stripe's onboarding. The link is followed inside the same
 * Effect that fetched it, so it never lands in React state.
 */
export const openOnboarding = runtime.fn(
  Effect.fnUntraced(function*() {
    const url = yield* onboardingLink;
    yield* Effect.sync(() => {
      window.location.assign(url);
    });
  }),
);

/** Into the driver's Stripe Express dashboard, by a single-use login link. */
export const openDashboard = runtime.fn(
  Effect.fnUntraced(function*() {
    const url = yield* dashboardLink;
    yield* Effect.sync(() => {
      window.location.assign(url);
    });
  }),
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
