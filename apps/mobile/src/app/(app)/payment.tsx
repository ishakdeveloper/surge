import { sessionAtom } from "@/atom/session-atoms.js";
import { IconBubble, Sign, SignGlyph, SignIcon, SignText } from "@/components/sign/sign.js";
import { Button } from "@/components/ui/button.js";
import { Text } from "@/components/ui/text.js";
import { CardActionError, CardSummary, useCardAction } from "@/ride/card-summary.js";
import { useAtomMount, useAtomValue } from "@effect/atom-react";
import Ionicons from "@expo/vector-icons/Ionicons";
import { cardAtom, paymentPushesAtom } from "@surge/common/atom/payment-atoms";
import { AsyncResult } from "effect/unstable/reactivity";
import { router } from "expo-router";
import { View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";

/**
 * How a trip gets paid for: the one card every fare is held on, and the way to
 * add or replace it.
 *
 * One method, because one is what Surge has. Nothing here stands in for a
 * wallet, a bank transfer or cash: the payments service holds and charges
 * cards, and a row for anything else would be a promise the system cannot
 * keep.
 */
const PaymentSheet = () => {
  // A card saved through Stripe arrives by webhook, so the summary changes
  // under this sheet while it is open.
  useAtomMount(paymentPushesAtom);
  const role = useAtomValue(
    sessionAtom,
    (session) => AsyncResult.isSuccess(session) ? session.value.role : undefined,
  );
  const saved = useAtomValue(cardAtom, (card) => AsyncResult.isSuccess(card) && card.value.saved);
  const action = useCardAction();
  const insets = useSafeAreaInsets();

  return (
    <View
      className="gap-6 bg-card px-5 pt-8"
      style={{ paddingBottom: Math.max(insets.bottom, 16) + 8 }}
    >
      <View className="gap-1">
        <Text accessibilityRole="header" className="text-2xl font-semibold">Payment</Text>
        <Text className="text-[15px] leading-[22px] text-muted-foreground">
          {role === "driver"
            ? "Riders pay by card. What you earn is paid out to your bank."
            : "Your fare is held on this card when you book and charged when the trip ends. A cancelled trip releases the hold."}
        </Text>
      </View>

      {role === "driver"
        ? (
          <Sign>
            <IconBubble name="wallet-outline" />
            <SignText className="min-w-0 flex-1 leading-5">
              Your balance and payouts are on the Earnings tab.
            </SignText>
          </Sign>
        )
        : (
          <View className="gap-2">
            <Text className="text-[13px] font-semibold text-muted-foreground">Payment method</Text>
            <Sign tone="service">
              <SignGlyph>
                <SignIcon name="card-outline" />
              </SignGlyph>
              <View className="min-w-0 flex-1">
                <CardSummary />
              </View>
              {saved && <Ionicons name="checkmark-circle" size={22} color="#ffffff" />}
            </Sign>
            <CardActionError />
            {action.available && (
              <Button
                variant={saved ? "outline" : "default"}
                disabled={action.waiting}
                onPress={action.run}
              >
                {action.label === "Change" ? "Change card" : action.label}
              </Button>
            )}
          </View>
        )}

      <Button
        variant="ghost"
        onPress={() => {
          router.back();
        }}
      >
        Done
      </Button>
    </View>
  );
};

export default PaymentSheet;
