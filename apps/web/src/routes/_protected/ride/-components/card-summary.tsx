import { QueryError } from "@/components/app/query-error.js";
import { useAtomValue } from "@effect/atom-react";
import { cardAtom } from "@surge/common/atom/payment-atoms";
import { formatExpiry } from "@surge/common/lib/format";
import { AsyncResult } from "effect/unstable/reactivity";

/** Stripe's brand slugs, as they are printed on the card. */
const BRANDS: Readonly<Record<string, string>> = {
  visa: "Visa",
  mastercard: "Mastercard",
  amex: "American Express",
  maestro: "Maestro",
};

/**
 * The rider's saved card, in whatever sign it stands on: it takes its colour
 * from the sign, so it reads the same on the black service sign and on white.
 * Read where it is shown, not handed down.
 */
export const CardSummary = () => {
  const card = useAtomValue(cardAtom);

  if (AsyncResult.isInitial(card)) {
    return <p className="text-[15px] opacity-70">Loading your card…</p>;
  }
  if (AsyncResult.isFailure(card)) return <QueryError result={card} subject="your card" />;
  if (!card.value.saved) {
    return (
      <p className="flex flex-col">
        <span className="text-[17px] font-bold">No card yet</span>
        <span className="text-[13px] opacity-70">Your fare is held on it when you book.</span>
      </p>
    );
  }

  const { brand, last4, expMonth, expYear } = card.value.card;
  return (
    <p className="flex flex-col">
      <span className="text-[17px] font-bold">
        {BRANDS[brand] ?? brand} ending <span className="tabular-nums">{last4}</span>
      </span>
      <span className="text-[13px] opacity-70">
        Expires <span className="tabular-nums">{formatExpiry(expMonth, expYear)}</span>
      </span>
    </p>
  );
};
