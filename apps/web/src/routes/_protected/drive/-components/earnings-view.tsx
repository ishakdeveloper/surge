import { openDashboard, openOnboarding } from "@/atom/payment-atoms.js";
import { ActionError } from "@/components/app/action-error.js";
import { QueryError } from "@/components/app/query-error.js";
import { Badge } from "@/components/ui/badge.js";
import { Button } from "@/components/ui/button.js";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table.js";
import { publishableKey } from "@/lib/stripe.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { loadConnectAndInitialize } from "@stripe/connect-js";
import { ConnectComponentsProvider, ConnectNotificationBanner } from "@stripe/react-connect-js";
import {
  balanceAtom,
  createAccountSession,
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
import * as React from "react";

export const EarningsView = () => (
  <div className="flex min-h-0 flex-1 flex-col gap-6 overflow-y-auto">
    <header className="flex items-start justify-between gap-4">
      <div className="flex flex-col gap-1">
        <h1 className="text-lg font-semibold">Earnings</h1>
        <p className="text-muted-foreground text-sm">
          Each trip's fare, less the platform's commission, goes to your balance when the rider is
          charged. Withdraw it to your bank whenever you like.
        </p>
      </div>
      <DashboardButton />
    </header>
    <PayoutAccount />
    <BalancePanel />
    <EarningsTable />
    <WithdrawalsTable />
  </div>
);

/**
 * Stripe's Express dashboard, for bank details and tax documents. Offered once
 * there is an account to open it on, and only against Stripe: the fake
 * processor's accounts have no dashboard to open.
 */
const DashboardButton = () => {
  const started = useAtomValue(
    payoutAccountAtom,
    (account) =>
      AsyncResult.isSuccess(account)
      && account.value.status !== "PAYOUT_ACCOUNT_STATUS_NOT_STARTED",
  );
  const opening = useAtomValue(openDashboard);
  const open = useAtomSet(openDashboard);
  if (!started || Option.isNone(publishableKey)) return null;

  return (
    <div className="flex shrink-0 flex-col items-end gap-2">
      <Button
        type="button"
        variant="outline"
        size="sm"
        disabled={opening.waiting}
        onClick={() => {
          open();
        }}
      >
        {opening.waiting ? "Opening Stripe…" : "Open Stripe Express"}
      </Button>
      {AsyncResult.isFailure(opening) && <ActionError cause={opening.cause} />}
    </div>
  );
};

const PayoutAccount = () => {
  const account = useAtomValue(payoutAccountAtom);
  const opening = useAtomValue(openOnboarding);
  const open = useAtomSet(openOnboarding);

  const body = (() => {
    if (AsyncResult.isInitial(account)) {
      return <p className="text-muted-foreground text-sm">Loading your payout account…</p>;
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
        <p className="text-sm">{message}</p>
        {(notStarted || requirementsDue) && (
          <div>
            <Button
              type="button"
              disabled={opening.waiting}
              onClick={() => {
                open();
              }}
            >
              {opening.waiting
                ? "Opening Stripe…"
                : notStarted
                ? "Set up payouts"
                : "Continue with Stripe"}
            </Button>
          </div>
        )}
        {AsyncResult.isFailure(opening) && <ActionError cause={opening.cause} />}
        {/* Stripe requires this wherever a connected account's health is shown. */}
        {!notStarted && Option.isSome(publishableKey) && (
          <NotificationBanner publishableKey={publishableKey.value} />
        )}
      </>
    );
  })();

  return (
    <section
      className="flex flex-col gap-3 rounded-md border border-border p-4"
      aria-labelledby="payouts"
    >
      <div className="flex items-center justify-between">
        <h2 id="payouts" className="text-sm font-medium">Payouts</h2>
        {AsyncResult.isSuccess(account) && (
          <Badge
            variant={account.value.status === "PAYOUT_ACCOUNT_STATUS_ACTIVE"
              ? "default"
              : "outline"}
            title={account.value.status}
          >
            {payoutStatus[account.value.status]}
          </Badge>
        )}
      </div>
      {body}
    </section>
  );
};

/**
 * Stripe's own notices about the account — a document to upload, a detail to
 * confirm — rendered by Connect.js in Stripe's frame. It fetches a fresh
 * account session each time it needs one.
 */
const NotificationBanner = (props: { readonly publishableKey: string; }) => {
  const createSession = useAtomSet(createAccountSession, { mode: "promise" });
  // One instance for the component's life: Connect.js loads its script and
  // holds the session on it.
  const [connect] = React.useState(() =>
    loadConnectAndInitialize({
      publishableKey: props.publishableKey,
      fetchClientSecret: () => createSession(),
    })
  );

  return (
    <ConnectComponentsProvider connectInstance={connect}>
      <ConnectNotificationBanner />
    </ConnectComponentsProvider>
  );
};

/** A fresh key per withdrawal, kept across retries of that one. */
// A click handler needs the key now, not an Effect to run for it; it only has
// to be unique, which this is.
// oxlint-disable-next-line effecttsgo/crypto-random-uuid
const newKey = () => crypto.randomUUID();

const BalancePanel = () => {
  const balance = useAtomValue(balanceAtom);
  const withdrawing = useAtomValue(withdraw);
  const request = useAtomSet(withdraw, { mode: "promiseExit" });
  const [key, setKey] = React.useState(newKey);

  const body = (() => {
    if (AsyncResult.isInitial(balance)) {
      return <p className="text-muted-foreground text-sm">Loading your balance…</p>;
    }
    if (AsyncResult.isFailure(balance)) {
      return <QueryError result={balance} subject="your balance" />;
    }

    const { availableCents, pendingCents, owedCents, canWithdraw } = balance.value;
    return (
      <>
        <dl className="grid grid-cols-3 gap-4">
          <Figure label="Available" cents={availableCents} hint="Yours to withdraw now." />
          <Figure
            label="Pending"
            cents={pendingCents}
            hint="Charged, and on its way to your balance."
          />
          <Figure
            label="Owed"
            cents={owedCents}
            hint="Earned, and paid in once payouts are set up."
          />
        </dl>
        {AsyncResult.isFailure(withdrawing) && <ActionError cause={withdrawing.cause} />}
        <div>
          <Button
            type="button"
            disabled={!canWithdraw || availableCents <= 0 || withdrawing.waiting}
            onClick={() => {
              void request({ idempotencyKey: key }).then((exit) => {
                // The next withdrawal is a new one; a failed one keeps its key,
                // so trying again cannot pay out twice.
                if (Exit.isSuccess(exit)) setKey(newKey());
              });
            }}
          >
            {withdrawing.waiting ? "Withdrawing…" : `Withdraw ${formatCents(availableCents)}`}
          </Button>
        </div>
      </>
    );
  })();

  return (
    <section
      className="flex flex-col gap-3 rounded-md border border-border p-4"
      aria-labelledby="balance"
    >
      <h2 id="balance" className="text-sm font-medium">Balance</h2>
      {body}
    </section>
  );
};

const Figure = (
  props: { readonly label: string; readonly cents: number; readonly hint: string; },
) => (
  <div className="flex flex-col gap-0.5">
    <dt className="text-muted-foreground text-xs">{props.label}</dt>
    <dd className="text-lg font-semibold tabular-nums">{formatCents(props.cents)}</dd>
    <dd className="text-muted-foreground text-xs">{props.hint}</dd>
  </div>
);

const EarningsTable = () => {
  const earnings = useAtomValue(earningsAtom);

  const body = (() => {
    if (AsyncResult.isInitial(earnings)) {
      return <p className="text-muted-foreground text-sm">Loading your trips…</p>;
    }
    if (AsyncResult.isFailure(earnings)) {
      return <QueryError result={earnings} subject="your earnings" />;
    }

    const { earnings: rows, paidCents, owedCents, nextPageToken } = earnings.value;
    if (rows.length === 0) {
      return <p className="text-muted-foreground text-sm">No trips driven yet.</p>;
    }
    return (
      <>
        <p className="text-muted-foreground text-sm">
          <span className="tabular-nums">{formatCents(paidCents)}</span> paid to your balance,{" "}
          <span className="tabular-nums">{formatCents(owedCents)}</span> owed.
          {nextPageToken !== "" && ` Showing your latest ${PAGE_SIZE} trips.`}
        </p>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Trip</TableHead>
              <TableHead className="text-right">Fare</TableHead>
              <TableHead className="text-right">Commission</TableHead>
              <TableHead className="text-right">Yours</TableHead>
              <TableHead className="text-center">Status</TableHead>
              <TableHead className="text-right">Date</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((row) => (
              <TableRow key={row.tripId}>
                <TableCell className="text-muted-foreground max-w-48 truncate font-mono text-xs">
                  {row.tripId}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {formatCents(row.grossCents)}
                </TableCell>
                <TableCell className="text-muted-foreground text-right tabular-nums">
                  {formatCents(row.commissionCents)}
                </TableCell>
                <TableCell className="text-right font-medium tabular-nums">
                  {formatCents(row.netCents)}
                </TableCell>
                <TableCell className="text-center">
                  <Badge variant="outline" title={row.status}>{earningStatus[row.status]}</Badge>
                </TableCell>
                <TableCell className="text-right whitespace-nowrap tabular-nums">
                  {formatTime(row.createdAt)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </>
    );
  })();

  return (
    <section className="flex flex-col gap-3" aria-labelledby="trips">
      <h2 id="trips" className="text-sm font-medium">Trips</h2>
      {body}
    </section>
  );
};

const WithdrawalsTable = () => {
  const withdrawals = useAtomValue(withdrawalsAtom);

  const body = (() => {
    if (AsyncResult.isInitial(withdrawals)) {
      return <p className="text-muted-foreground text-sm">Loading your withdrawals…</p>;
    }
    if (AsyncResult.isFailure(withdrawals)) {
      return <QueryError result={withdrawals} subject="your withdrawals" />;
    }

    const { withdrawals: rows, nextPageToken } = withdrawals.value;
    if (rows.length === 0) {
      return <p className="text-muted-foreground text-sm">Nothing withdrawn yet.</p>;
    }
    return (
      <>
        {nextPageToken !== "" && (
          <p className="text-muted-foreground text-sm">
            Showing your latest {PAGE_SIZE} withdrawals.
          </p>
        )}
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Withdrawal</TableHead>
              <TableHead>Reason</TableHead>
              <TableHead className="text-right">Amount</TableHead>
              <TableHead className="text-center">Status</TableHead>
              <TableHead className="text-right">Requested</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((row) => (
              <TableRow key={row.id}>
                <TableCell className="text-muted-foreground max-w-48 truncate font-mono text-xs">
                  {row.id}
                </TableCell>
                <TableCell className="max-w-64 truncate">
                  {row.failureReason === "" ? "-" : row.failureReason}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {formatCents(row.amountCents)}
                </TableCell>
                <TableCell className="text-center">
                  <Badge
                    variant={row.status === "WITHDRAWAL_STATUS_FAILED" ? "destructive" : "outline"}
                    title={row.status}
                  >
                    {withdrawalStatus[row.status]}
                  </Badge>
                </TableCell>
                <TableCell className="text-right whitespace-nowrap tabular-nums">
                  {formatTime(row.createdAt)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </>
    );
  })();

  return (
    <section className="flex flex-col gap-3" aria-labelledby="withdrawals">
      <h2 id="withdrawals" className="text-sm font-medium">Withdrawals</h2>
      {body}
    </section>
  );
};
