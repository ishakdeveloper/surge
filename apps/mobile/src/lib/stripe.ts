import {
  handleNextAction,
  initPaymentSheet,
  PaymentSheetError,
  presentPaymentSheet,
} from "@stripe/stripe-react-native";
import { StripeRefused } from "@surge/common/payments/stripe-refused";
import { Effect } from "effect";

/**
 * Stripe's half of payments on the phone — what Stripe.js is in the browser.
 *
 * The card never touches this app's code or our servers: Stripe's own sheet
 * collects it, and Stripe tells the payments service by webhook. The result
 * reaches this app the same way it reaches the web, as a push.
 */

/**
 * Where a bank's app sends the rider back after a redirect-based check. The
 * scheme is the app's own; `app/+native-intent.ts` keeps the router from
 * treating it as a screen.
 */
export const STRIPE_RETURN_URL = "surge://stripe-redirect";

const unavailable = new StripeRefused({
  code: "stripe_unavailable",
  detail: "Stripe could not be reached. Check your connection and try again.",
});

const refused = (
  error: { readonly code: string; readonly message: string; readonly localizedMessage?: string; },
) => new StripeRefused({ code: error.code, detail: error.localizedMessage ?? error.message });

/**
 * Collect a card in Stripe's sheet, for the SetupIntent the payments service
 * made. `false` when the rider closed the sheet: that is a choice, not a
 * failure, and it deserves no red banner.
 */
export const collectCard = Effect.fnUntraced(function*(setupIntentClientSecret: string) {
  const prepared = yield* Effect.tryPromise({
    try: () =>
      initPaymentSheet({
        merchantDisplayName: "Surge",
        setupIntentClientSecret,
        returnURL: STRIPE_RETURN_URL,
      }),
    catch: () => unavailable,
  });
  if (prepared.error !== undefined) return yield* refused(prepared.error);

  const presented = yield* Effect.tryPromise({
    try: () => presentPaymentSheet(),
    catch: () => unavailable,
  });
  if (presented.error !== undefined) {
    if (presented.error.code === PaymentSheetError.Canceled) return false;
    return yield* refused(presented.error);
  }
  return true;
});

/**
 * Run the bank's check on a hold — 3-D Secure, usually — in Stripe's own UI.
 * The hold is a PaymentIntent, so this is the PaymentIntent flavour.
 */
export const confirmWithBank = Effect.fnUntraced(function*(paymentIntentClientSecret: string) {
  const result = yield* Effect.tryPromise({
    try: () => handleNextAction(paymentIntentClientSecret, STRIPE_RETURN_URL),
    catch: () => unavailable,
  });
  if (result.error !== undefined) return yield* refused(result.error);
});
