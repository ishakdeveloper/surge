import { sessionAtom } from "@/atom/session-atoms.js";
import { ProfileEditor } from "@/components/profile/profile-editor.js";
import { Button } from "@/components/ui/button.js";
import { Text } from "@/components/ui/text.js";
import { useAtomValue } from "@effect/atom-react";
import { AsyncResult } from "effect/unstable/reactivity";
import { router } from "expo-router";
import { Keyboard, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";

/**
 * The name and photo someone is seen by, as a short sheet that fits them —
 * opened from the account, from onboarding's reminder, from anywhere a face
 * is missing. Everything saves as it is made, so Done only puts it away.
 */
const ProfileSheet = () => {
  const role = useAtomValue(
    sessionAtom,
    (session) => AsyncResult.isSuccess(session) ? session.value.role : undefined,
  );
  const insets = useSafeAreaInsets();

  return (
    <View
      className="gap-6 bg-card px-5 pt-8"
      style={{ paddingBottom: Math.max(insets.bottom, 16) + 8 }}
    >
      <View className="gap-1">
        <Text accessibilityRole="header" className="text-2xl font-semibold">Your profile</Text>
        <Text className="text-[15px] text-muted-foreground">
          {role === "driver"
            ? "What a rider sees when you accept their trip."
            : "What your driver sees when they accept your trip."}
        </Text>
      </View>
      {role !== undefined && <ProfileEditor role={role} explain={false} />}
      <Button
        onPress={() => {
          Keyboard.dismiss();
          router.back();
        }}
      >
        Done
      </Button>
    </View>
  );
};

export default ProfileSheet;
