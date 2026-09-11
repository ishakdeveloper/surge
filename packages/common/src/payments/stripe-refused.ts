import { Schema } from "effect";

/**
 * Stripe said no, in its own words: a declined card, a failed bank check, a
 * form left half filled in.
 *
 * Its own error rather than the gateway's `ErrorBody` because it never reached
 * our servers — both apps talk to Stripe directly for these steps, Stripe.js in
 * the browser and Stripe's React Native SDK on the phone. Each app builds one
 * from its SDK's error; the screens show `detail` the way they show the
 * gateway's message.
 */
export class StripeRefused extends Schema.TaggedError<StripeRefused>()("StripeRefused", {
  code: Schema.String,
  detail: Schema.String,
}) {}
