import { colors } from "@/lib/theme.js";
import { Stack } from "expo-router";

export const unstable_settings = { initialRouteName: "sign-in" };

/**
 * One screen: signing in and signing up are the same code flow, so there is
 * nothing for a second screen to do.
 */
const AuthLayout = () => (
  <Stack
    screenOptions={{
      headerStyle: { backgroundColor: colors.background },
      headerTintColor: colors.foreground,
      headerShadowVisible: false,
      contentStyle: { backgroundColor: colors.background },
    }}
  >
    <Stack.Screen name="sign-in" options={{ title: "Surge" }} />
  </Stack>
);

export default AuthLayout;
