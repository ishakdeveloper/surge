import { confirmHoldAtom } from "@/atom/payment-atoms.js";
import { ActionError } from "@/components/app/action-error.js";
import { QueryError } from "@/components/app/query-error.js";
import { Button } from "@/components/ui/button.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { tripPaymentAtom } from "@surge/common/atom/payment-atoms";
import { errorCode } from "@surge/common/lib/cause";
import { formatCents, formatTime, paymentFailure, paymentStatus } from "@surge/common/lib/format";
import type { TripId } from "@surge/domain/api/Primitives";
import { awaitsRider } from "@surge/domain/payments/Payment";
import { Option } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";

/**
 * A trip that has no payment yet is an answer, not a fault: the payments
 * service has not consumed the booking yet, or the trip was booked while
 * payment was not required. Everything else is the real failure.
 */
const notFound = (result: AsyncResult.Failure<unknown, unknown>): boolean =>
  Option.getOrUndefined(errorCode(result.cause)) === "not_found";

/**
 * The hold, while a trip waits on it: nothing to do while the card is being
 * asked, a button when the bank wants the rider to confirm, the reason when it
 * failed. The trip leaves `payment_pending` by itself once the hold is placed,
 * and this unmounts with it.
 */
export const HoldStep = (props: { readonly tripId: TripId; readonly totalCents: number; }) => {
  const payment = useAtomValue(tripPaymentAtom(props.tripId));
  const confirming = useAtomValue(confirmHoldAtom(props.tripId));
  const confirm = useAtomSet(confirmHoldAtom(props.tripId));

  const placing = (
    <p className="text-muted-foreground text-sm">
      Placing a hold of <span className="tabular-nums">{formatCents(props.totalCents)}</span>{" "}
      on your card. You are charged when the trip ends.
    </p>
  );

  if (AsyncResult.isInitial(payment)) return placing;
  if (AsyncResult.isFailure(payment)) {
    return notFound(payment) ? placing : <QueryError result={payment} subject="the payment" />;
  }

  const current = payment.value;
  if (awaitsRider(current)) {
    return (
      <section
        className="flex flex-col gap-3 rounded-md border border-border p-4"
        aria-labelledby="bank-check"
      >
        <h2 id="bank-check" className="text-sm font-medium">Your bank wants to confirm</h2>
        <p className="text-muted-foreground text-sm">
          Confirm the hold of{" "}
          <span className="tabular-nums">{formatCents(current.amountCents)}</span>{" "}
          with your bank, and a driver is found as soon as it is placed.
        </p>
        {AsyncResult.isFailure(confirming) && <ActionError cause={confirming.cause} />}
        <Button
          type="button"
          disabled={confirming.waiting}
          onClick={() => {
            confirm(current.clientSecret);
          }}
        >
          {confirming.waiting ? "Waiting for your bank…" : "Confirm with your bank"}
        </Button>
      </section>
    );
  }
  return current.status === "PAYMENT_STATUS_FAILED"
    ? <p className="text-destructive text-sm">{paymentFailure(current.failureReason)}</p>
    : placing;
};

/**
 * What a finished trip cost: the receipt for a completed one, and for a
 * cancelled one whether anything was taken.
 */
export const TripReceipt = (props: { readonly tripId: TripId; }) => {
  const payment = useAtomValue(tripPaymentAtom(props.tripId));

  if (AsyncResult.isInitial(payment)) {
    return <p className="text-muted-foreground text-sm">Loading the payment…</p>;
  }
  if (AsyncResult.isFailure(payment)) {
    return notFound(payment)
      ? <p className="text-muted-foreground text-sm">No payment was taken for this trip.</p>
      : <QueryError result={payment} subject="the payment" />;
  }

  const receipt = payment.value;
  return (
    <dl className="grid grid-cols-[5.5rem_1fr] gap-1.5 text-sm">
      <dt className="text-muted-foreground">Payment</dt>
      <dd>
        {paymentStatus[receipt.status]}{" "}
        <span className="text-muted-foreground font-mono text-xs">{receipt.status}</span>
      </dd>
      {receipt.capturedCents > 0 && (
        <>
          <dt className="text-muted-foreground">Charged</dt>
          <dd className="tabular-nums">{formatCents(receipt.capturedCents)}</dd>
        </>
      )}
      {receipt.refundedCents > 0 && (
        <>
          <dt className="text-muted-foreground">Refunded</dt>
          <dd className="tabular-nums">{formatCents(receipt.refundedCents)}</dd>
        </>
      )}
      {receipt.failureReason !== "" && (
        <>
          <dt className="text-muted-foreground">Why</dt>
          <dd>{paymentFailure(receipt.failureReason)}</dd>
        </>
      )}
      <dt className="text-muted-foreground">When</dt>
      <dd className="tabular-nums">{formatTime(receipt.updatedAt)}</dd>
      <dt className="text-muted-foreground">Reference</dt>
      <dd className="text-muted-foreground font-mono text-xs">{receipt.id}</dd>
    </dl>
  );
};
