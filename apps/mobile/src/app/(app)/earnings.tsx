import { Screen } from "@/components/app/screen.js";
import { Text } from "@/components/ui/text.js";
import { BalancePanel, EarningsList, PayoutAccount, WithdrawalsList } from "@/drive/earnings.js";
import { useAtomMount } from "@effect/atom-react";
import { paymentPushesAtom } from "@surge/common/atom/payment-atoms";

/**
 * What a driver has earned and been paid. Kept fresh by payment pushes, which
 * is how a charge at the end of a trip moves the balance while this is open.
 */
const Earnings = () => {
  useAtomMount(paymentPushesAtom);

  return (
    <Screen>
      <Text className="text-muted-foreground">
        Each trip's fare, less the platform's commission, goes to your balance when the rider is
        charged. Withdraw it to your bank whenever you like.
      </Text>
      <PayoutAccount />
      <BalancePanel />
      <EarningsList />
      <WithdrawalsList />
    </Screen>
  );
};

export default Earnings;
