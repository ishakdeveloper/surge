import { AuthToken } from "@/AuthToken.js";
import { SurgeApi } from "@/SurgeApi.js";
import { describe, expect, it } from "@effect/vitest";
import { Effect, Layer } from "effect";
import { FetchHttpClient } from "effect/unstable/http";
import { type AuthServer, outboxOpen, signInWithCode } from "./support/sign-in-with-code.js";

/**
 * A driver's payout account, through the gateway to the payments service and on
 * to its processor — Stripe's test mode, or the fake that holds to it.
 *
 * What the Earnings screens stand on: onboarding opens Stripe's own pages, and
 * the account session is what Stripe's notification banner is drawn from, in
 * the browser and in the app's WebView alike. The processor's side of both is
 * held to Stripe by the payments service's own contract test; this proves the
 * route to it — the driver's token, the gateway's mapping, the refusal before
 * there is an account.
 *
 * Skips unless the gateway, the payments service and an auth service with its
 * dev outbox are all running.
 */
const authBase = process.env["AUTH_BASE_URL"] ?? "http://localhost:3200";
const gateway = process.env["SURGE_API_URL"] ?? "http://localhost:8100";
const paymentsMetrics = process.env["PAYMENTS_METRICS_URL"] ?? "http://localhost:9107";

const auth: AuthServer = {
  base: authBase,
  origin: process.env["WEB_URL"] ?? "http://localhost:5273",
};

const reachable = async (url: string): Promise<boolean> => {
  try {
    return (await fetch(url, { signal: AbortSignal.timeout(2000) })).ok;
  } catch {
    return false;
  }
};

const online = await reachable(`${gateway}/health`)
  && await reachable(`${authBase}/health`)
  && await reachable(`${paymentsMetrics}/metrics`)
  && await outboxOpen(auth);

/** A fresh driver, because a payout account is made once per driver. */
const token = online
  ? await signInWithCode(auth, `payouts-${Date.now()}@surge.test`, "driver")
  : "";

const layer = SurgeApi.layer.pipe(
  Layer.provide(
    Layer.succeed(AuthToken)({
      get: Effect.succeed(token),
      identity: Effect.die("not needed here"),
      invalidate: Effect.void,
    }),
  ),
  Layer.provide(FetchHttpClient.layer),
);

describe.skipIf(!online)("a driver's payout account, end to end", () => {
  /**
   * One test rather than three, because each step needs the one before it and
   * the order is the point: no session before there is an account to show.
   */
  it.effect("refuses a banner session until payouts are set up, then issues one", () =>
    Effect.gen(function*() {
      const api = yield* SurgeApi;

      const early = yield* Effect.flip(api.payments.createAccountSession({ payload: {} }));
      expect(early).toMatchObject({ error: { code: "failed_precondition" } });

      const onboarding = yield* api.payments.startOnboarding({ payload: {} });
      expect(onboarding.url).toMatch(/^https:\/\//);

      const { account } = yield* api.payments.getPayoutAccount();
      expect(account.status).not.toBe("PAYOUT_ACCOUNT_STATUS_NOT_STARTED");

      // Stripe's account session secrets, and the fake's, begin the same way.
      const session = yield* api.payments.createAccountSession({ payload: {} });
      expect(session.clientSecret).toMatch(/^accs_/);
    }).pipe(Effect.provide(layer)));
});
