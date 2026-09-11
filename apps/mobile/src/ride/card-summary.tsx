import { addCard } from "@/atom/payment-atoms.js";
import { ActionError, QueryError } from "@/components/app/errors.js";
import { Action, SignText } from "@/components/sign/sign.js";
import { stripeConfigured } from "@/lib/config.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { cardAtom, startCardSetup } from "@surge/common/atom/payment-atoms";
import { formatExpiry } from "@surge/common/lib/format";
import { AsyncResult } from "effect/unstable/reactivity";
import { View } from "react-native";

/** Stripe's brand slugs, as they are printed on the card. */
const BRANDS: Readonly<Record<string, string>> = {
  visa: "Visa",
  mastercard: "Mastercard",
  amex: "American Express",
  maestro: "Maestro",
};

/**
 * The rider's saved card, in the ink of whatever card it stands on. Read where
 * it is shown, not handed down.
 */
export const CardSummary = () => {
  const card = useAtomValue(cardAtom);

  if (AsyncResult.isInitial(card)) {
    return <SignText className="opacity-70">Loading your card…</SignText>;
  }
  if (AsyncResult.isFailure(card)) return <QueryError result={card} subject="your card" />;
  if (!card.value.saved) {
    return (
      <View>
        <SignText className="text-base font-semibold">No card yet</SignText>
        <SignText className="text-[13px] opacity-70">
          {stripeConfigured
            ? "Your fare is held on it when you book."
            : "Test payments: this saves a Visa ending 4242, which every hold succeeds on."}
        </SignText>
      </View>
    );
  }

  const { brand, last4, expMonth, expYear } = card.value.card;
  return (
    <View>
      <SignText className="text-base font-semibold">
        {BRANDS[brand] ?? brand} ending <SignText className="tabular-nums">{last4}</SignText>
      </SignText>
      <SignText className="text-[13px] opacity-70">
        Expires <SignText className="tabular-nums">{formatExpiry(expMonth, expYear)}</SignText>
      </SignText>
    </View>
  );
};

/**
 * Adding or replacing the card: Stripe's sheet, or — against the fake
 * processor, which has no Stripe to open — saving the test Visa. Stripe
 * reports the card by webhook, so the summary changes when the push arrives;
 * closing the sheet without a card is not an error.
 */
export const useCardAction = () => {
  const saved = useAtomValue(cardAtom, (card) => AsyncResult.isSuccess(card) && card.value.saved);
  const adding = useAtomValue(addCard);
  const add = useAtomSet(addCard);
  const saving = useAtomValue(startCardSetup);
  const save = useAtomSet(startCardSetup);
  const waiting = stripeConfigured ? adding.waiting : saving.waiting;

  return {
    saved,
    waiting,
    /** Against the fake processor a saved card has nothing to be replaced with. */
    available: stripeConfigured || !saved,
    label: waiting
      ? stripeConfigured ? "Opening Stripe…" : "Saving…"
      : saved
      ? "Change"
      : stripeConfigured
      ? "Add a card"
      : "Save test card",
    run: () => {
      if (stripeConfigured) add();
      else save();
    },
  } as const;
};

/** The card's own action, as a small yellow pill on the black card. */
export const CardAction = () => {
  const action = useCardAction();
  if (!action.available) return null;
  return (
    <Action
      label={action.label}
      onPress={action.run}
      tone="primary"
      size="sm"
      disabled={action.waiting}
    />
  );
};

/** Why adding the card failed, said under the card it came from. */
export const CardActionError = () => {
  const adding = useAtomValue(addCard);
  const saving = useAtomValue(startCardSetup);

  if (AsyncResult.isFailure(adding)) return <ActionError cause={adding.cause} />;
  if (AsyncResult.isFailure(saving)) return <ActionError cause={saving.cause} />;
  return null;
};
