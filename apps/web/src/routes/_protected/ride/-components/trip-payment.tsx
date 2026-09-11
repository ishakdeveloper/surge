import { confirmHoldAtom } from "@/atom/payment-atoms.js";
import { ActionError } from "@/components/app/action-error.js";
import { QueryError } from "@/components/app/query-error.js";
import { Sign, SignGlyph } from "@/components/sign/sign.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { tripPaymentAtom } from "@surge/common/atom/payment-atoms";
import { errorCode } from "@surge/common/lib/cause";
import { formatCents, formatTime, paymentFailure, paymentStatus } from "@surge/common/lib/format";
import type { TripId } from "@surge/domain/api/Primitives";
import { awaitsRider } from "@surge/domain/payments/Payment";
import { Option } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";
import { CreditCard, LoaderCircle, X } from "lucide-react";

/**
 * A trip that has no payment yet is an answer, not a fault: the payments
 * service has not consumed the booking yet, or the trip was booked while
 * payment was not required. Everything else is the real failure.
 */
const notFound = (result: AsyncResult.Failure<unknown, unknown>): boolean =>
  Option.getOrUndefined(errorCode(result.cause)) === "not_found";

/**
 * The hold, while a trip waits on it, on the black service sign: nothing to do
 * while the card is being asked, a button when the bank wants the rider to
 * confirm, and a red sign with the reason when it failed. The trip leaves
 * `payment_pending` by itself once the hold is placed, and this unmounts with it.
 */
export const HoldStep = (props: { readonly tripId: TripId; readonly totalCents: number; }) => {
  const payment = useAtomValue(tripPaymentAtom(props.tripId));
  const confirming = useAtomValue(confirmHoldAtom(props.tripId));
  const confirm = useAtomSet(confirmHoldAtom(props.tripId));

  const placing = (
    <Sign tone="service" className="items-center">
      <SignGlyph>
        <LoaderCircle className="motion-safe:animate-spin" />
      </SignGlyph>
      <p className="text-[15px]">
        Placing a hold of{" "}
        <span className="font-bold tabular-nums">{formatCents(props.totalCents)}</span>{" "}
        on your card. You are charged when the trip ends.
      </p>
    </Sign>
  );

  if (AsyncResult.isInitial(payment)) return placing;
  if (AsyncResult.isFailure(payment)) {
    return notFound(payment) ? placing : <QueryError result={payment} subject="the payment" />;
  }

  const current = payment.value;
  if (awaitsRider(current)) {
    return (
      <Sign tone="service" className="flex-col gap-3" aria-labelledby="bank-check">
        <div className="flex gap-3">
          <SignGlyph>
            <CreditCard />
          </SignGlyph>
          <div className="flex flex-col gap-1">
            <h2 id="bank-check" className="text-[17px] font-bold">Your bank wants to confirm</h2>
            <p className="text-[15px] opacity-80">
              Confirm the hold of{" "}
              <span className="tabular-nums">{formatCents(current.amountCents)}</span>{" "}
              with your bank, and a driver is found as soon as it is placed.
            </p>
          </div>
        </div>
        {AsyncResult.isFailure(confirming) && <ActionError cause={confirming.cause} />}
        <button
          type="button"
          disabled={confirming.waiting}
          onClick={() => {
            confirm(current.clientSecret);
          }}
          className="h-12 cursor-pointer rounded-full bg-primary px-4 text-[17px] font-bold text-primary-foreground transition-colors active:bg-[#f0c400] disabled:cursor-not-allowed disabled:bg-white/15 disabled:text-white/70"
        >
          {confirming.waiting ? "Waiting for your bank…" : "Confirm with your bank"}
        </button>
      </Sign>
    );
  }
  return current.status === "PAYMENT_STATUS_FAILED"
    ? (
      <Sign tone="refused" className="items-center">
        <SignGlyph>
          <X />
        </SignGlyph>
        <p className="text-[15px] font-semibold">{paymentFailure(current.failureReason)}</p>
      </Sign>
    )
    : placing;
};

/**
 * What a finished trip cost: the receipt for a completed one, and for a
 * cancelled one whether anything was taken.
 */
export const TripReceipt = (props: { readonly tripId: TripId; }) => {
  const payment = useAtomValue(tripPaymentAtom(props.tripId));

  if (AsyncResult.isInitial(payment)) {
    return <p className="text-sm text-muted-foreground">Loading the payment…</p>;
  }
  if (AsyncResult.isFailure(payment)) {
    return notFound(payment)
      ? <p className="text-sm text-muted-foreground">No payment was taken for this trip.</p>
      : <QueryError result={payment} subject="the payment" />;
  }

  const receipt = payment.value;
  return (
    <dl className="grid grid-cols-[6rem_1fr] gap-x-3 gap-y-1.5 text-[15px]">
      <dt className="text-muted-foreground">Payment</dt>
      <dd className="font-semibold">{paymentStatus[receipt.status]}</dd>
      {receipt.capturedCents > 0 && (
        <>
          <dt className="text-muted-foreground">Charged</dt>
          <dd className="font-semibold tabular-nums">{formatCents(receipt.capturedCents)}</dd>
        </>
      )}
      {receipt.refundedCents > 0 && (
        <>
          <dt className="text-muted-foreground">Refunded</dt>
          <dd className="font-semibold tabular-nums">{formatCents(receipt.refundedCents)}</dd>
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
      <dd className="truncate font-mono text-xs leading-6 text-muted-foreground">{receipt.id}</dd>
    </dl>
  );
};
