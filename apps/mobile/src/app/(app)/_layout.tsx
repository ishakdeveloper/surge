import { colors } from "@/lib/theme.js";
import { Stack } from "expo-router";

/** A conversation opened from a notification still has the tabs beneath it to go back to. */
export const unstable_settings = { initialRouteName: "(tabs)" };

/**
 * The tabs, and what rises over them: a conversation and a note to support
 * as the system's page sheet — swiped down to put away, as a ride app's chat
 * is — and the profile as a short sheet that fits what is on it.
 */
const AppLayout = () => (
  <Stack
    screenOptions={{ headerShown: false, contentStyle: { backgroundColor: colors.background } }}
  >
    <Stack.Screen name="(tabs)" />
    <Stack.Screen
      name="conversation/[id]"
      options={{ presentation: "modal", contentStyle: { backgroundColor: colors.card } }}
    />
    <Stack.Screen
      name="support/new"
      options={{ presentation: "modal", contentStyle: { backgroundColor: colors.card } }}
    />
    <Stack.Screen
      name="profile"
      options={{
        presentation: "formSheet",
        sheetAllowedDetents: "fitToContents",
        sheetGrabberVisible: true,
        sheetCornerRadius: 28,
        contentStyle: { backgroundColor: colors.card },
      }}
    />
    <Stack.Screen
      name="payment"
      options={{
        presentation: "formSheet",
        sheetAllowedDetents: "fitToContents",
        sheetGrabberVisible: true,
        sheetCornerRadius: 28,
        contentStyle: { backgroundColor: colors.card },
      }}
    />
    <Stack.Screen
      name="onboarding"
      options={{ presentation: "fullScreenModal", gestureEnabled: false }}
    />
  </Stack>
);

export default AppLayout;
