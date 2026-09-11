import { confirmHoldAtom } from "@/atom/payment-atoms.js";
import { ActionError, QueryError } from "@/components/app/errors.js";
import { Sign, SignGlyph, SignIcon, SignText } from "@/components/sign/sign.js";
import { Text } from "@/components/ui/text.js";
import { colors } from "@/lib/theme.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { tripPaymentAtom } from "@surge/common/atom/payment-atoms";
import { errorCode } from "@surge/common/lib/cause";
import { formatCents, formatTime, paymentFailure, paymentStatus } from "@surge/common/lib/format";
import type { TripId } from "@surge/domain/api/Primitives";
import { awaitsRider } from "@surge/domain/payments/Payment";
import { Option } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";
import type * as React from "react";
import { ActivityIndicator, Pressable, View } from "react-native";

/**
 * A trip that has no payment yet is an answer, not a fault: the payments
 * service has not consumed the booking yet.
 */
const notFound = (result: AsyncResult.Failure<unknown, unknown>): boolean =>
  Option.getOrUndefined(errorCode(result.cause)) === "not_found";

/**
 * The hold, while a trip waits on it, on the black service sign. The trip
 * leaves `payment_pending` by itself once the hold is placed, and this
 * unmounts with it.
 */
export const HoldStep = (props: { readonly tripId: TripId; readonly totalCents: number; }) => {
  const payment = useAtomValue(tripPaymentAtom(props.tripId));
  const confirming = useAtomValue(confirmHoldAtom(props.tripId));
  const confirm = useAtomSet(confirmHoldAtom(props.tripId));

  const placing = (
    <Sign tone="service">
      <SignGlyph>
        <ActivityIndicator color="#ffffff" />
      </SignGlyph>
      <SignText className="flex-1">
        Placing a hold of{" "}
        <SignText className="font-bold tabular-nums">{formatCents(props.totalCents)}</SignText>{" "}
        on your card. You are charged when the trip ends.
      </SignText>
    </Sign>
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
      <Sign tone="service" className="flex-col items-stretch gap-3">
        <View className="flex-row gap-3">
          <SignGlyph>
            <SignIcon name="card-outline" />
          </SignGlyph>
          <View className="flex-1 gap-1">
            <SignText accessibilityRole="header" className="text-[17px] font-bold">
              Your bank wants to confirm
            </SignText>
            <SignText className="opacity-80">
              Confirm the hold of{" "}
              <SignText className="tabular-nums">{formatCents(current.amountCents)}</SignText>{" "}
              with your bank, and a driver is found as soon as it is placed.
            </SignText>
          </View>
        </View>
        {AsyncResult.isFailure(confirming) && <ActionError cause={confirming.cause} />}
        <Pressable
          accessibilityRole="button"
          accessibilityState={{ disabled: confirming.waiting }}
          disabled={confirming.waiting}
          onPress={() => {
            confirm(current.clientSecret);
          }}
          className="h-12 items-center justify-center rounded-full bg-primary px-4 active:scale-[0.97] active:bg-[#f0c400] disabled:bg-white/15"
        >
          <Text
            className={confirming.waiting
              ? "text-[17px] font-bold text-white/70"
              : "text-[17px] font-bold text-primary-foreground"}
          >
            {confirming.waiting ? "Waiting for your bank…" : "Confirm with your bank"}
          </Text>
        </Pressable>
      </Sign>
    );
  }
  return current.status === "PAYMENT_STATUS_FAILED"
    ? (
      <Sign tone="refused">
        <SignGlyph>
          <SignIcon name="close" />
        </SignGlyph>
        <SignText className="flex-1 font-semibold">
          {paymentFailure(current.failureReason)}
        </SignText>
      </Sign>
    )
    : placing;
};

const Row = (props: { readonly label: string; readonly children: React.ReactNode; }) => (
  <View className="flex-row gap-3">
    <Text className="w-24 text-[15px] text-muted-foreground">{props.label}</Text>
    <Text className="flex-1 text-[15px] font-semibold">{props.children}</Text>
  </View>
);

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
    <View className="gap-1.5">
      <Row label="Payment">{paymentStatus[receipt.status]}</Row>
      {receipt.capturedCents > 0 && <Row label="Charged">{formatCents(receipt.capturedCents)}</Row>}
      {receipt.refundedCents > 0 && <Row label="Refunded">{formatCents(receipt.refundedCents)}
      </Row>}
      {receipt.failureReason !== "" && (
        <Row label="Why">{paymentFailure(receipt.failureReason)}</Row>
      )}
      <Row label="When">{formatTime(receipt.updatedAt)}</Row>
      <View className="flex-row gap-3">
        <Text className="w-24 text-[15px] text-muted-foreground">Reference</Text>
        <Text
          numberOfLines={1}
          style={{ color: colors.mutedForeground }}
          className="flex-1 font-mono text-xs leading-6"
        >
          {receipt.id}
        </Text>
      </View>
    </View>
  );
};
