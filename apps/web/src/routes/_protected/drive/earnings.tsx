import { paymentPushesAtom } from "@/atom/payment-atoms.js";
import { sessionAtom } from "@/atom/session-atoms.js";
import { EarningsView } from "@/routes/_protected/drive/-components/earnings-view.js";
import { useAtomMount, useAtomValue } from "@effect/atom-react";
import { createFileRoute } from "@tanstack/react-router";
import { AsyncResult } from "effect/unstable/reactivity";

/**
 * The driver's money: whether they can be paid, what they have, what each
 * trip earned, and what has gone to their bank.
 *
 * Upwork's shape rather than a ride-hailing app's weekly payout: each trip's
 * share lands in the driver's Stripe balance as the trip is charged, and the
 * driver takes it out when they choose.
 */
const Earnings = () => {
  useAtomMount(paymentPushesAtom);
  const role = useAtomValue(
    sessionAtom,
    (session) => AsyncResult.isSuccess(session) ? session.value.role : undefined,
  );

  // The payments service refuses a rider here regardless; this only says so
  // before anybody asks.
  if (role !== undefined && role !== "driver") {
    return (
      <p className="text-muted-foreground text-sm">
        This page is for drivers. Your account rides — your card is on the Payment page.
      </p>
    );
  }

  return <EarningsView />;
};

export const Route = createFileRoute("/_protected/drive/earnings")({
  // Client-only: Stripe's Connect.js runs in the browser and nowhere else.
  ssr: false,
  staticData: { crumb: "Earnings" },
  component: Earnings,
});
