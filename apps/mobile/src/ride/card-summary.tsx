import { addCard } from "@/atom/payment-atoms.js";
import { ActionError, QueryError } from "@/components/app/errors.js";
import { Button } from "@/components/ui/button.js";
import { Text } from "@/components/ui/text.js";
import { stripeConfigured } from "@/lib/config.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { cardAtom, startCardSetup } from "@surge/common/atom/payment-atoms";
import { formatExpiry } from "@surge/common/lib/format";
import { AsyncResult } from "effect/unstable/reactivity";
import { View } from "react-native";

/**
 * The rider's saved card, as one line, and the way to add or replace it. Read
 * where it is shown, not handed down.
 */
export const CardSummary = () => {
  const card = useAtomValue(cardAtom);

  if (AsyncResult.isInitial(card)) {
    return <Text className="text-muted-foreground">Loading your card…</Text>;
  }
  if (AsyncResult.isFailure(card)) return <QueryError result={card} subject="your card" />;
  if (!card.value.saved) return <NoCard />;

  const { brand, last4, expMonth, expYear } = card.value.card;
  return (
    <View className="gap-3">
      <Text>
        <Text className="font-mono text-xs">{brand}</Text> ending{" "}
        <Text className="tabular-nums">{last4}</Text>
        <Text className="text-muted-foreground">
          {" "}· expires <Text className="tabular-nums">{formatExpiry(expMonth, expYear)}</Text>
        </Text>
      </Text>
      {stripeConfigured && <AddCard label="Replace your card" />}
    </View>
  );
};

/**
 * No card yet. Against Stripe, its sheet collects one; against the fake
 * processor there is no Stripe to open, and the fake saves its test Visa.
 */
const NoCard = () => (
  <View className="gap-3">
    <Text className="text-muted-foreground">
      {stripeConfigured
        ? "No card saved. The fare is held on it when you book."
        : "No card saved. Payments are running against the fake processor, which saves its test Visa ending 4242 — every hold succeeds on it."}
    </Text>
    {stripeConfigured ? <AddCard label="Add a card" /> : <SaveTestCard />}
  </View>
);

/**
 * Stripe's sheet. The card is saved by Stripe and reported by webhook, so the
 * summary above changes when the push arrives — closing the sheet without a
 * card is not an error and shows none.
 */
const AddCard = (props: { readonly label: string; }) => {
  const adding = useAtomValue(addCard);
  const add = useAtomSet(addCard);

  return (
    <>
      {AsyncResult.isFailure(adding) && <ActionError cause={adding.cause} />}
      <Button
        variant="outline"
        disabled={adding.waiting}
        onPress={() => {
          add();
        }}
      >
        {adding.waiting ? "Opening Stripe…" : props.label}
      </Button>
    </>
  );
};

const SaveTestCard = () => {
  const saving = useAtomValue(startCardSetup);
  const save = useAtomSet(startCardSetup);

  return (
    <>
      {AsyncResult.isFailure(saving) && <ActionError cause={saving.cause} />}
      <Button
        variant="outline"
        disabled={saving.waiting}
        onPress={() => {
          save();
        }}
      >
        {saving.waiting ? "Saving…" : "Save the test card"}
      </Button>
    </>
  );
};
