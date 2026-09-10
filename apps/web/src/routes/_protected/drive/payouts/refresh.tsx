import { openOnboarding } from "@/atom/payment-atoms.js";
import { ActionError } from "@/components/app/action-error.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { createFileRoute, Link } from "@tanstack/react-router";
import { AsyncResult } from "effect/unstable/reactivity";
import * as React from "react";

/**
 * Where Stripe sends a driver whose onboarding link has expired or was already
 * used. Links are single use, so the only thing to do is make a new one and go
 * straight back.
 */
const Refresh = () => {
  const opening = useAtomValue(openOnboarding);
  const open = useAtomSet(openOnboarding);

  React.useEffect(() => {
    open();
  }, [open]);

  if (AsyncResult.isFailure(opening)) {
    return (
      <div className="flex max-w-md flex-col gap-3">
        <ActionError cause={opening.cause} />
        <Link to="/drive/earnings" className="text-sm underline underline-offset-4">
          Back to earnings
        </Link>
      </div>
    );
  }
  return <output className="text-muted-foreground text-sm">Taking you back to Stripe…</output>;
};

export const Route = createFileRoute("/_protected/drive/payouts/refresh")({
  ssr: false,
  staticData: { crumb: "Payouts" },
  component: Refresh,
});
