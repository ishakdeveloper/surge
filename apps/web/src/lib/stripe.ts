import { loadStripe, type Stripe, type StripeError } from "@stripe/stripe-js";
import { Option, Schema } from "effect";

/**
 * Stripe's browser half, when there is a Stripe to talk to.
 *
 * Read straight from `import.meta.env` rather than through the runtime's
 * `ConfigProvider`: nothing in `packages/client` needs it, and the components
 * that do — Stripe's own Elements and Connect providers — take a key, not an
 * Effect.
 *
 * Unset means the payments service is running its fake processor. There is no
 * Stripe account behind it for Stripe.js to load, so the pages save the fake's
 * test card with a plain button and render none of Stripe's components.
 */
export const publishableKey: Option.Option<string> = Option.fromNullishOr(
  import.meta.env.VITE_STRIPE_PUBLISHABLE_KEY,
).pipe(Option.filter((key) => key !== ""));

/**
 * One Stripe.js for the page. `loadStripe` injects Stripe's script tag, and
 * Stripe asks for it once per page rather than once per form: the same
 * instance collects the card and later runs the bank's confirmation step.
 */
export const stripe: Option.Option<Promise<Stripe | null>> = Option.map(publishableKey, loadStripe);

/**
 * Stripe said no, in its own words: a declined card, a failed bank check, a
 * form left half filled in.
 *
 * Its own error rather than the gateway's `ErrorBody` because it never reached
 * our servers — Stripe.js answers the browser directly, and `ActionError`
 * shows `detail` the way it shows the gateway's message.
 */
export class StripeRefused extends Schema.TaggedError<StripeRefused>()("StripeRefused", {
  code: Schema.String,
  detail: Schema.String,
}) {
  static fromStripe(error: StripeError): StripeRefused {
    return new StripeRefused({
      code: error.code ?? error.type,
      detail: error.message ?? "Stripe gave no reason.",
    });
  }
}
