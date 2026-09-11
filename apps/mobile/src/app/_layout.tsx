// First, for its side effect: the background location task must be defined in
// the bundle's global scope before anything mounts. See the module.
import "@/lib/background-location.js";
import "@/global.css";
import { sessionAtom } from "@/atom/session-atoms.js";
import { SessionScope } from "@/iam/session-scope.js";
import { stripePublishableKey } from "@/lib/config.js";
import { colors } from "@/lib/theme.js";
import { useAtomValue } from "@effect/atom-react";
import { StripeProvider, useStripe } from "@stripe/stripe-react-native";
import { Option } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";
import { Stack } from "expo-router";
import { StatusBar } from "expo-status-bar";
import * as React from "react";
import { ActivityIndicator, Linking, View } from "react-native";

/**
 * Signed in or not, decided once per session scope.
 *
 * `Stack.Protected` is the whole guard: the `(app)` group does not exist for a
 * signed-out session and `(auth)` does not exist for a signed-in one, so there
 * is no redirect to race and a deep link into the app while signed out lands
 * on sign-in. As on the web, this protects screens, not data — every Go
 * service verifies the token on every request regardless.
 */
const Navigator = () => {
  const session = useAtomValue(sessionAtom);

  if (AsyncResult.isInitial(session)) {
    return (
      <View className="flex-1 items-center justify-center bg-background">
        <ActivityIndicator color={colors.mutedForeground} accessibilityLabel="Loading" />
      </View>
    );
  }

  const signedIn = AsyncResult.isSuccess(session);

  return (
    <Stack
      screenOptions={{ headerShown: false, contentStyle: { backgroundColor: colors.background } }}
    >
      <Stack.Protected guard={signedIn}>
        <Stack.Screen name="(app)" />
      </Stack.Protected>
      <Stack.Protected guard={!signedIn}>
        <Stack.Screen name="(auth)" />
      </Stack.Protected>
    </Stack>
  );
};

/**
 * Hands a bank's redirect back to Stripe, which finishes the check it belongs
 * to. `app/+native-intent.ts` keeps the same URL away from the router.
 */
const StripeRedirects = () => {
  const { handleURLCallback } = useStripe();

  React.useEffect(() => {
    const subscription = Linking.addEventListener("url", (event) => {
      void handleURLCallback(event.url);
    });
    return () => {
      subscription.remove();
    };
  }, [handleURLCallback]);

  return null;
};

/**
 * Stripe's SDK, when payments run against Stripe. Without a key the payments
 * service runs its fake processor and nothing here talks to Stripe.
 */
const Payments = (props: { readonly children: React.ReactNode; }) =>
  Option.match(stripePublishableKey, {
    onNone: () => <>{props.children}</>,
    onSome: (publishableKey) => (
      <StripeProvider publishableKey={publishableKey} urlScheme="surge">
        <>
          <StripeRedirects />
          {props.children}
        </>
      </StripeProvider>
    ),
  });

const RootLayout = () => (
  <Payments>
    <SessionScope>
      <StatusBar style="dark" />
      <Navigator />
    </SessionScope>
  </Payments>
);

export default RootLayout;
