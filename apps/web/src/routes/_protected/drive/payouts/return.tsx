import { createFileRoute, redirect } from "@tanstack/react-router";

/**
 * Where Stripe sends a driver after onboarding. There is nothing to show here:
 * Stripe comes back to this URL whether or not the driver finished, so having
 * arrived proves nothing. The earnings page reads the account's real status
 * from the payments service, which learns it from Stripe's own webhook.
 */
export const Route = createFileRoute("/_protected/drive/payouts/return")({
  beforeLoad: () => {
    // oxlint-disable-next-line typescript/only-throw-error
    throw redirect({ to: "/drive/earnings" });
  },
});
