import type { SurgeApi } from "@/SurgeApi.js";
import { Effect } from "effect";

/**
 * A rider's card, saved the way the apps save one, for a test whose booking has
 * to be paid for — with `TRIP_REQUIRE_PAYMENT`, a trip waits in
 * `payment_pending` until the payments service holds the fare, and nothing
 * reaches the matcher before that.
 *
 * Against the fake processor, starting the setup is the card saved. Against
 * Stripe, the setup is confirmed with Stripe's test Visa — with the publishable
 * key and the setup's client secret, which is exactly what Stripe.js and the
 * app's card sheet send — and Stripe then reports the card by webhook, which the
 * payments service only hears when Stripe's events are forwarded to the gateway.
 */
export const saveATestCard = Effect.fnUntraced(function*(api: SurgeApi["Service"]) {
  const { clientSecret } = yield* api.payments.createSetupIntent({ payload: {} });
  if ((yield* api.payments.getMethod()).saved) return;

  const publishableKey = process.env["VITE_STRIPE_PUBLISHABLE_KEY"] ?? "";
  if (publishableKey === "") {
    return yield* Effect.die(
      new Error(
        "Payments runs against Stripe, and saving a test card needs VITE_STRIPE_PUBLISHABLE_KEY.",
      ),
    );
  }

  const setupIntent = clientSecret.slice(0, clientSecret.indexOf("_secret_"));
  const confirmed = yield* Effect.promise(async () => {
    const response = await fetch(
      `https://api.stripe.com/v1/setup_intents/${setupIntent}/confirm`,
      {
        method: "POST",
        headers: {
          authorization: `Basic ${btoa(`${publishableKey}:`)}`,
          "content-type": "application/x-www-form-urlencoded",
        },
        body: new URLSearchParams({ client_secret: clientSecret, payment_method: "pm_card_visa" }),
      },
    );
    return (await response.json()) as { status?: string; error?: { message: string; }; };
  });
  if (confirmed.status !== "succeeded") {
    return yield* Effect.die(
      new Error(
        `Stripe did not confirm the test card: ${confirmed.error?.message ?? confirmed.status}`,
      ),
    );
  }

  for (let attempt = 0; attempt < 40; attempt++) {
    if ((yield* api.payments.getMethod()).saved) return;
    yield* Effect.sleep("500 millis");
  }
  return yield* Effect.die(
    new Error(
      "Stripe confirmed the test card and the payments service never heard of it. Stripe reports a saved card by webhook — is `make stripe-listen` running?",
    ),
  );
});
