import { confirmHoldAtom } from "@/atom/payment-atoms.js";
import { Detail, Details } from "@/components/app/details.js";
import { ActionError, QueryError } from "@/components/app/errors.js";
import { Button } from "@/components/ui/button.js";
import { Card, CardDescription, CardTitle } from "@/components/ui/card.js";
import { Text } from "@/components/ui/text.js";
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
 * service has not consumed the booking yet.
 */
const notFound = (result: AsyncResult.Failure<unknown, unknown>): boolean =>
  Option.getOrUndefined(errorCode(result.cause)) === "not_found";

/**
 * The hold, while a trip waits on it. The trip leaves `payment_pending` by
 * itself once the hold is placed, and this unmounts with it.
 */
export const HoldStep = (props: { readonly tripId: TripId; readonly totalCents: number; }) => {
  const payment = useAtomValue(tripPaymentAtom(props.tripId));
  const confirming = useAtomValue(confirmHoldAtom(props.tripId));
  const confirm = useAtomSet(confirmHoldAtom(props.tripId));

  const placing = (
    <Text className="text-muted-foreground">
      Placing a hold of <Text className="tabular-nums">{formatCents(props.totalCents)}</Text>{" "}
      on your card. You are charged when the trip ends.
    </Text>
  );

  if (AsyncResult.isInitial(payment)) return placing;
  if (AsyncResult.isFailure(payment)) {
    return notFound(payment) ? placing : <QueryError result={payment} subject="the payment" />;
  }

  const current = payment.value;
  if (awaitsRider(current)) {
    /**
     * The rider starts the bank's check with a tap rather than it opening by
     * itself: it is a context switch to their bank, and one they should see
     * coming. The trip moves on by itself once the hold is placed.
     */
    return (
      <Card>
        <CardTitle className="text-sm">Your bank wants to confirm</CardTitle>
        <CardDescription>
          Confirm the hold of{" "}
          <Text className="tabular-nums">{formatCents(current.amountCents)}</Text>{" "}
          with your bank, and a driver is found as soon as it is placed.
        </CardDescription>
        {AsyncResult.isFailure(confirming) && <ActionError cause={confirming.cause} />}
        <Button
          disabled={confirming.waiting}
          onPress={() => {
            confirm(current.clientSecret);
          }}
        >
          {confirming.waiting ? "Waiting for your bank…" : "Confirm with your bank"}
        </Button>
      </Card>
    );
  }
  return current.status === "PAYMENT_STATUS_FAILED"
    ? <Text className="text-destructive">{paymentFailure(current.failureReason)}</Text>
    : placing;
};

/** What a finished trip cost, or whether anything was taken for a cancelled one. */
export const TripReceipt = (props: { readonly tripId: TripId; }) => {
  const payment = useAtomValue(tripPaymentAtom(props.tripId));

  if (AsyncResult.isInitial(payment)) {
    return <Text className="text-muted-foreground">Loading the payment…</Text>;
  }
  if (AsyncResult.isFailure(payment)) {
    return notFound(payment)
      ? <Text className="text-muted-foreground">No payment was taken for this trip.</Text>
      : <QueryError result={payment} subject="the payment" />;
  }

  const receipt = payment.value;
  return (
    <Details>
      <Detail label="Payment" value={paymentStatus[receipt.status]} />
      {receipt.capturedCents > 0 && (
        <Detail label="Charged" value={formatCents(receipt.capturedCents)} />
      )}
      {receipt.refundedCents > 0 && (
        <Detail label="Refunded" value={formatCents(receipt.refundedCents)} />
      )}
      {receipt.failureReason !== "" && (
        <Detail label="Why" value={paymentFailure(receipt.failureReason)} />
      )}
      <Detail label="When" value={formatTime(receipt.updatedAt)} />
      <Detail label="Reference" value={receipt.id} mono />
    </Details>
  );
};
