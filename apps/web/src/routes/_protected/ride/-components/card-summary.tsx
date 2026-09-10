import { cardAtom } from "@/atom/payment-atoms.js";
import { QueryError } from "@/components/app/query-error.js";
import { formatExpiry } from "@/lib/format.js";
import { useAtomValue } from "@effect/atom-react";
import { AsyncResult } from "effect/unstable/reactivity";

/** The rider's saved card, as one line. Read where it is shown, not handed down. */
export const CardSummary = () => {
  const card = useAtomValue(cardAtom);

  if (AsyncResult.isInitial(card)) {
    return <p className="text-muted-foreground text-sm">Loading your card…</p>;
  }
  if (AsyncResult.isFailure(card)) return <QueryError result={card} subject="your card" />;
  if (!card.value.saved) return <p className="text-muted-foreground text-sm">No card saved.</p>;

  const { brand, last4, expMonth, expYear } = card.value.card;
  return (
    <p className="text-sm">
      <span className="font-mono text-xs">{brand}</span> ending{" "}
      <span className="tabular-nums">{last4}</span>
      <span className="text-muted-foreground">
        {" "}· expires <span className="tabular-nums">{formatExpiry(expMonth, expYear)}</span>
      </span>
    </p>
  );
};
