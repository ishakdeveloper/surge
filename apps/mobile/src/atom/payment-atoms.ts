import { collectCard, confirmWithBank } from "@/lib/stripe.js";
import { SurgeApi } from "@surge/client/SurgeApi";
import { dashboardLink, onboardingLink } from "@surge/common/atom/payment-atoms";
import { Keys } from "@surge/common/atom/reactivity-keys";
import { runtime } from "@surge/common/atom/runtime";
import type { TripId } from "@surge/domain/api/Primitives";
import { Effect } from "effect";
import { Atom } from "effect/unstable/reactivity";
import * as WebBrowser from "expo-web-browser";

/**
 * The phone's half of payments — `apps/web/src/atom/payment-atoms.ts` is the
 * browser's. Everything that talks to our own services is shared, in
 * `@surge/common/atom/payment-atoms`; what is here drives Stripe's native UI or
 * follows Stripe's links.
 *
 * Each announces `Keys.payments` when it finishes, so the card, the hold or the
 * payout account is read again at once. The webhook's push does the same a
 * moment later, and whichever lands second finds nothing new.
 */

/** Ask for a SetupIntent, then collect the card in Stripe's sheet. `false` if the rider backed out. */
export const addCard = runtime.fn(
  Effect.fnUntraced(function*() {
    const api = yield* SurgeApi;
    const { clientSecret } = yield* api.payments.createSetupIntent({ payload: {} });
    return yield* collectCard(clientSecret);
  }),
  { reactivityKeys: [Keys.payments] },
);

/** The bank's check on one trip's hold, started by the rider rather than opening by itself. */
export const confirmHoldAtom = Atom.family((_tripId: TripId) =>
  runtime.fn((clientSecret: string) => confirmWithBank(clientSecret), {
    reactivityKeys: [Keys.payments],
  })
);

/**
 * Stripe's hosted onboarding, in an in-app browser. The link is followed inside
 * the Effect that fetched it, so it never lands in React state. The browser
 * closing is the cue to read the account again — on iOS the promise settles
 * when it closes; on Android at once, and the webhook's push covers the rest.
 */
export const openOnboarding = runtime.fn(
  Effect.fnUntraced(function*() {
    const url = yield* onboardingLink;
    yield* Effect.promise(() => WebBrowser.openBrowserAsync(url));
  }),
  { reactivityKeys: [Keys.payments] },
);

/** Into the driver's Stripe Express dashboard, by a single-use login link. */
export const openDashboard = runtime.fn(
  Effect.fnUntraced(function*() {
    const url = yield* dashboardLink;
    yield* Effect.promise(() => WebBrowser.openBrowserAsync(url));
  }),
);
