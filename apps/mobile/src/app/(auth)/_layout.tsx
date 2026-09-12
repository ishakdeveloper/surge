import { colors } from "@/lib/theme.js";
import { Stack } from "expo-router";

export const unstable_settings = { initialRouteName: "sign-in" };

/**
 * One screen: signing in and signing up are the same code flow, so there is
 * nothing for a second screen to do. No header — the screen opens with its
 * own mark and question.
 */
const AuthLayout = () => (
  <Stack
    screenOptions={{
      headerShown: false,
      contentStyle: { backgroundColor: colors.background },
    }}
  >
    <Stack.Screen name="sign-in" />
  </Stack>
);

export default AuthLayout;
