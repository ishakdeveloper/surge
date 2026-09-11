import { sessionAtom } from "@/atom/session-atoms.js";
import { CardSetup } from "@/components/payments/card-setup.js";
import { IconBubble, Sign } from "@/components/sign/sign.js";
import { CardSummary } from "@/routes/_protected/ride/-components/card-summary.js";
import { useAtomMount, useAtomValue } from "@effect/atom-react";
import { paymentPushesAtom } from "@surge/common/atom/payment-atoms";
import { createFileRoute, Link } from "@tanstack/react-router";
import { AsyncResult } from "effect/unstable/reactivity";
import { CreditCard } from "lucide-react";

/**
 * The rider's card: the one every hold is placed on.
 *
 * A fare is held on it when a trip is booked and charged when the trip ends;
 * a cancelled or unmatched trip releases the hold. Adding or replacing it is
 * `CardSetup`, which the rider's onboarding asks for too.
 */
const PaymentPage = () => {
  useAtomMount(paymentPushesAtom);
  const role = useAtomValue(
    sessionAtom,
    (session) => AsyncResult.isSuccess(session) ? session.value.role : undefined,
  );

  if (role === "driver") {
    return (
      <p className="text-[15px] text-muted-foreground">
        This page is for riders. Your account drives, and your pay is on the{" "}
        <Link to="/drive/earnings" className="font-semibold text-foreground underline">
          Earnings
        </Link>{" "}
        page.
      </p>
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
      {/* The rider's white sheet, as on the ride page: the black card tile needs a surface. */}
      <div className="mx-auto flex w-full max-w-md flex-col gap-4 rounded-3xl bg-card p-5 shadow-[0_1px_2px_rgb(0_0_0/0.04),0_12px_32px_-18px_rgb(0_0_0/0.22)]">
        <header className="flex flex-col gap-1 px-1">
          <h1 className="text-2xl font-bold">Payment</h1>
          <p className="text-[15px] text-muted-foreground">
            Your fare is held on this card when you book and charged when the trip ends. A cancelled
            trip releases the hold.
          </p>
        </header>

        <Sign tone="service" className="items-center">
          <IconBubble className="bg-white/10">
            <CreditCard />
          </IconBubble>
          <div className="min-w-0 flex-1">
            <CardSummary />
          </div>
        </Sign>

        <section aria-label="Add or replace your card" className="flex flex-col gap-3 px-1">
          <CardSetup />
        </section>
      </div>
    </div>
  );
};

export const Route = createFileRoute("/_protected/ride/payment")({
  // Client-only: Stripe.js runs in the browser and nowhere else.
  ssr: false,
  staticData: { crumb: "Payment" },
  component: PaymentPage,
});
