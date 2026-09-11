import { openDashboard, openOnboarding } from "@/atom/payment-atoms.js";
import { ActionError, QueryError } from "@/components/app/errors.js";
import { Badge } from "@/components/ui/badge.js";
import { Button } from "@/components/ui/button.js";
import { Card } from "@/components/ui/card.js";
import { Heading, Text } from "@/components/ui/text.js";
import { ConnectBanner } from "@/drive/connect-banner.js";
import { stripeConfigured, stripePublishableKey } from "@/lib/config.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import {
  balanceAtom,
  earningsAtom,
  PAGE_SIZE,
  payoutAccountAtom,
  withdraw,
  withdrawalsAtom,
} from "@surge/common/atom/payment-atoms";
import {
  earningStatus,
  formatCents,
  formatTime,
  payoutStatus,
  withdrawalStatus,
} from "@surge/common/lib/format";
import { Exit, Option } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";
import { randomUUID } from "expo-crypto";
import * as React from "react";
import { View } from "react-native";

/**
 * A driver's money — `apps/web/.../earnings-view.tsx`, as lists rather than
 * tables, over the same shared atoms. Each part reads its own atom, so one that
 * fails to load does not take the others with it.
 */

/** Whether the driver can be paid yet, and the way into Stripe when they cannot. */
export const PayoutAccount = () => {
  const account = useAtomValue(payoutAccountAtom);
  const onboarding = useAtomValue(openOnboarding);
  const onboard = useAtomSet(openOnboarding);
  const dashboard = useAtomValue(openDashboard);
  const openStripe = useAtomSet(openDashboard);

  const body = (() => {
    if (AsyncResult.isInitial(account)) {
      return <Text className="text-muted-foreground">Loading your payout account…</Text>;
    }
    if (AsyncResult.isFailure(account)) {
      return <QueryError result={account} subject="your payout account" />;
    }

    const { status, requirementsDue } = account.value;
    const notStarted = status === "PAYOUT_ACCOUNT_STATUS_NOT_STARTED";
    const message = notStarted
      ? "Set up payouts with Stripe to get paid. What you earn before then is kept, and paid once you finish."
      : requirementsDue
      ? "Stripe needs more from you before you can be paid. Nothing you have earned is lost meanwhile."
      : status === "PAYOUT_ACCOUNT_STATUS_PENDING"
      ? "Stripe is checking your details. You are paid as soon as it has."
      : "Your share of each trip goes to your balance as the rider is charged.";

    return (
      <>
        <Text>{message}</Text>
        {(notStarted || requirementsDue) && (
          <Button
            disabled={onboarding.waiting}
            onPress={() => {
              onboard();
            }}
          >
            {onboarding.waiting
              ? "Opening Stripe…"
              : notStarted
              ? "Set up payouts"
              : "Continue with Stripe"}
          </Button>
        )}
        {AsyncResult.isFailure(onboarding) && <ActionError cause={onboarding.cause} />}
        {/* The fake processor's accounts have no dashboard to open. */}
        {!notStarted && stripeConfigured && (
          <Button
            variant="outline"
            disabled={dashboard.waiting}
            onPress={() => {
              openStripe();
            }}
          >
            {dashboard.waiting ? "Opening Stripe…" : "Open Stripe Express"}
          </Button>
        )}
        {AsyncResult.isFailure(dashboard) && <ActionError cause={dashboard.cause} />}
        {/* Stripe requires this wherever a connected account's health is shown. */}
        {!notStarted && Option.isSome(stripePublishableKey) && (
          <ConnectBanner publishableKey={stripePublishableKey.value} />
        )}
      </>
    );
  })();

  return (
    <Card>
      <View className="flex-row items-center justify-between">
        <Heading>Payouts</Heading>
        {AsyncResult.isSuccess(account) && (
          <Badge
            variant={account.value.status === "PAYOUT_ACCOUNT_STATUS_ACTIVE"
              ? "default"
              : "outline"}
          >
            {payoutStatus[account.value.status]}
          </Badge>
        )}
      </View>
      {body}
    </Card>
  );
};

/** A fresh key per withdrawal, kept across retries of that one. */
const newKey = () => randomUUID();

export const BalancePanel = () => {
  const balance = useAtomValue(balanceAtom);
  const withdrawing = useAtomValue(withdraw);
  const request = useAtomSet(withdraw, { mode: "promiseExit" });
  const [key, setKey] = React.useState(newKey);

  const body = (() => {
    if (AsyncResult.isInitial(balance)) {
      return <Text className="text-muted-foreground">Loading your balance…</Text>;
    }
    if (AsyncResult.isFailure(balance)) {
      return <QueryError result={balance} subject="your balance" />;
    }

    const { availableCents, pendingCents, owedCents, canWithdraw } = balance.value;
    return (
      <>
        <View className="flex-row gap-3">
          <Figure label="Available" cents={availableCents} hint="Yours to withdraw now." />
          <Figure label="Pending" cents={pendingCents} hint="Charged, on its way." />
          <Figure label="Owed" cents={owedCents} hint="Paid once payouts are set up." />
        </View>
        {AsyncResult.isFailure(withdrawing) && <ActionError cause={withdrawing.cause} />}
        <Button
          disabled={!canWithdraw || availableCents <= 0 || withdrawing.waiting}
          onPress={() => {
            void request({ idempotencyKey: key }).then((exit) => {
              // The next withdrawal is a new one; a failed one keeps its key,
              // so trying again cannot pay out twice.
              if (Exit.isSuccess(exit)) setKey(newKey());
            });
          }}
        >
          {withdrawing.waiting ? "Withdrawing…" : `Withdraw ${formatCents(availableCents)}`}
        </Button>
      </>
    );
  })();

  return (
    <Card>
      <Heading>Balance</Heading>
      {body}
    </Card>
  );
};

const Figure = (
  props: { readonly label: string; readonly cents: number; readonly hint: string; },
) => (
  <View className="flex-1 gap-0.5">
    <Text className="text-xs text-muted-foreground">{props.label}</Text>
    <Text className="text-lg font-semibold tabular-nums">{formatCents(props.cents)}</Text>
    <Text className="text-xs text-muted-foreground">{props.hint}</Text>
  </View>
);

/** The driver's share of each trip, newest first. */
export const EarningsList = () => {
  const earnings = useAtomValue(earningsAtom);

  const body = (() => {
    if (AsyncResult.isInitial(earnings)) {
      return <Text className="text-muted-foreground">Loading your trips…</Text>;
    }
    if (AsyncResult.isFailure(earnings)) {
      return <QueryError result={earnings} subject="your earnings" />;
    }

    const { earnings: rows, paidCents, owedCents, nextPageToken } = earnings.value;
    if (rows.length === 0) {
      return <Text className="text-muted-foreground">No trips driven yet.</Text>;
    }

    return (
      <>
        <Text className="text-muted-foreground">
          <Text className="tabular-nums">{formatCents(paidCents)}</Text> paid to your balance,{" "}
          <Text className="tabular-nums">{formatCents(owedCents)}</Text> owed.
          {nextPageToken !== "" && ` Showing your latest ${PAGE_SIZE} trips.`}
        </Text>
        {rows.map((row) => (
          <View key={row.tripId} className="gap-1 border-t border-border pt-3">
            <View className="flex-row items-center justify-between">
              <Text className="font-medium tabular-nums">{formatCents(row.netCents)}</Text>
              <Badge variant="outline">{earningStatus[row.status]}</Badge>
            </View>
            <Text className="text-xs text-muted-foreground">
              {formatCents(row.grossCents)} fare, {formatCents(row.commissionCents)} commission ·
              {" "}
              {formatTime(row.createdAt)}
            </Text>
            <Text selectable className="font-mono text-xs text-muted-foreground">{row.tripId}</Text>
          </View>
        ))}
      </>
    );
  })();

  return (
    <View className="gap-3">
      <Heading>Trips</Heading>
      {body}
    </View>
  );
};

export const WithdrawalsList = () => {
  const withdrawals = useAtomValue(withdrawalsAtom);

  const body = (() => {
    if (AsyncResult.isInitial(withdrawals)) {
      return <Text className="text-muted-foreground">Loading your withdrawals…</Text>;
    }
    if (AsyncResult.isFailure(withdrawals)) {
      return <QueryError result={withdrawals} subject="your withdrawals" />;
    }

    const { withdrawals: rows, nextPageToken } = withdrawals.value;
    if (rows.length === 0) {
      return <Text className="text-muted-foreground">Nothing withdrawn yet.</Text>;
    }

    return (
      <>
        {nextPageToken !== "" && (
          <Text className="text-muted-foreground">
            Showing your latest {PAGE_SIZE} withdrawals.
          </Text>
        )}
        {rows.map((row) => (
          <View key={row.id} className="gap-1 border-t border-border pt-3">
            <View className="flex-row items-center justify-between">
              <Text className="font-medium tabular-nums">{formatCents(row.amountCents)}</Text>
              <Badge
                variant={row.status === "WITHDRAWAL_STATUS_FAILED" ? "destructive" : "outline"}
              >
                {withdrawalStatus[row.status]}
              </Badge>
            </View>
            <Text className="text-xs text-muted-foreground">
              Requested {formatTime(row.createdAt)}
              {row.failureReason === "" ? "" : ` · ${row.failureReason}`}
            </Text>
          </View>
        ))}
      </>
    );
  })();

  return (
    <View className="gap-3">
      <Heading>Withdrawals</Heading>
      {body}
    </View>
  );
};
