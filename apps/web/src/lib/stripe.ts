import { publicConfig } from "@/lib/public-config.js";
import { loadStripe, type Stripe, type StripeError } from "@stripe/stripe-js";
import { StripeRefused } from "@surge/common/payments/stripe-refused";
import { Option } from "effect";

/**
 * Stripe's browser half, when there is a Stripe to talk to.
 *
 * Read straight from `publicConfig` rather than through the runtime's
 * `ConfigProvider`: nothing in `packages/client` needs it, and the components
 * that do — Stripe's own Elements and Connect providers — take a key, not an
 * Effect.
 *
 * Unset means the payments service is running its fake processor. There is no
 * Stripe account behind it for Stripe.js to load, so the pages save the fake's
 * test card with a plain button and render none of Stripe's components.
 */
export const publishableKey: Option.Option<string> = Option.fromNullishOr(
  publicConfig.STRIPE_PUBLISHABLE_KEY,
).pipe(Option.filter((key) => key !== ""));

/**
 * One Stripe.js for the page. `loadStripe` injects Stripe's script tag, and
 * Stripe asks for it once per page rather than once per form: the same
 * instance collects the card and later runs the bank's confirmation step.
 */
export const stripe: Option.Option<Promise<Stripe | null>> = Option.map(publishableKey, loadStripe);

/** Stripe.js's error, as the refusal both apps show. */
export const refusedBy = (error: StripeError): StripeRefused =>
  new StripeRefused({
    code: error.code ?? error.type,
    detail: error.message ?? "Stripe gave no reason.",
  });
