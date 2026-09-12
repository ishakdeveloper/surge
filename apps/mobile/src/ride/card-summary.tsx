import { addCard } from "@/atom/payment-atoms.js";
import { ActionError, QueryError } from "@/components/app/errors.js";
import { PressableScale } from "@/components/motion/pressable-scale.js";
import { Action, SignText } from "@/components/sign/sign.js";
import { Text } from "@/components/ui/text.js";
import { stripeConfigured } from "@/lib/config.js";
import { colors } from "@/lib/theme.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import Ionicons from "@expo/vector-icons/Ionicons";
import { cardAtom, startCardSetup } from "@surge/common/atom/payment-atoms";
import { formatExpiry } from "@surge/common/lib/format";
import { AsyncResult } from "effect/unstable/reactivity";
import { router } from "expo-router";
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
 * The card a fare will be held on, as one quiet row: the card itself, or what
 * to do about there not being one, and the way to the payment sheet. It sits
 * where a ride app puts the payment line — under the price, above the action
 * that commits to it.
 */
export const PaymentRow = () => {
  const card = useAtomValue(cardAtom);
  const saved = AsyncResult.isSuccess(card) && card.value.saved;
  const missing = !AsyncResult.isSuccess(card)
    ? "Payment"
    : stripeConfigured
    ? "Add a card"
    : "Save test card";

  return (
    <PressableScale
      accessibilityLabel={saved
        ? `Payment: ${
          BRANDS[card.value.card.brand] ?? card.value.card.brand
        } ending ${card.value.card.last4}. Change`
        : missing}
      accessibilityHint="Opens your payment methods"
      onPress={() => {
        router.push("/payment");
      }}
      className="h-14 flex-row items-center gap-3 rounded-2xl bg-tile px-4 active:bg-tile-hover"
    >
      {/* The card itself, as a card: a small white face with the chip's glyph. */}
      <View className="h-7 w-10 items-center justify-center rounded-md bg-card">
        <Ionicons name="card" size={15} color={colors.foreground} />
      </View>
      {saved
        ? (
          <View className="min-w-0 flex-1 flex-row items-center gap-2">
            <Text numberOfLines={1} className="text-[15px] font-semibold">
              {BRANDS[card.value.card.brand] ?? card.value.card.brand}
            </Text>
            {/* The digits a rider recognises, after the four dots a card is read by. */}
            <Text className="text-[15px] leading-[15px] text-muted-foreground">••••</Text>
            <Text className="text-[15px] font-semibold tabular-nums">
              {card.value.card.last4}
            </Text>
          </View>
        )
        : (
          <Text numberOfLines={1} className="min-w-0 flex-1 text-[15px] font-semibold">
            {missing}
          </Text>
        )}
      {saved
        ? <Text className="text-[13px] font-semibold text-muted-foreground">Change</Text>
        : <Ionicons name="chevron-forward" size={16} color={colors.mutedForeground} />}
    </PressableScale>
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
