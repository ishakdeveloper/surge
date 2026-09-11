import { sessionAtom } from "@/atom/session-atoms.js";
import { colors } from "@/lib/theme.js";
import { useAtomValue } from "@effect/atom-react";
import Ionicons from "@expo/vector-icons/Ionicons";
import { AsyncResult } from "effect/unstable/reactivity";
import { Tabs } from "expo-router";
import type * as React from "react";
import type { ColorValue } from "react-native";

const icon = (name: React.ComponentProps<typeof Ionicons>["name"]) =>
(
  props: { readonly color: ColorValue; readonly size: number; },
) => <Ionicons name={name} color={props.color} size={props.size} />;

/**
 * The signed-in app: one surface per role, and the account.
 *
 * A role sees only its own tab. The trip list is scoped by whoever holds the
 * token, so a driver on the Ride tab would see the trips they drove presented
 * as rides they booked — hiding the tab is the honest version of the web's
 * "this page is for riders" notice. `ops` has neither: the console is a web
 * surface.
 */
const AppLayout = () => {
  const role = useAtomValue(
    sessionAtom,
    (session) => AsyncResult.isSuccess(session) ? session.value.role : undefined,
  );

  return (
    <Tabs
      screenOptions={{
        headerStyle: { backgroundColor: colors.background },
        headerTintColor: colors.foreground,
        headerShadowVisible: false,
        sceneStyle: { backgroundColor: colors.background },
        // A black band, as on the web: navigation is a service, and the tab
        // you are on is the one sign-yellow thing in it.
        tabBarStyle: { backgroundColor: colors.secondary, borderTopColor: colors.secondary },
        tabBarActiveTintColor: colors.primary,
        tabBarInactiveTintColor: "rgba(255, 255, 255, 0.7)",
      }}
    >
      <Tabs.Screen name="index" options={{ href: null }} />
      <Tabs.Protected guard={role === "rider"}>
        {/* No header: the map runs to the top of the screen. */}
        <Tabs.Screen
          name="ride"
          options={{ title: "Ride", headerShown: false, tabBarIcon: icon("car-outline") }}
        />
      </Tabs.Protected>
      <Tabs.Protected guard={role === "driver"}>
        <Tabs.Screen
          name="drive"
          options={{ title: "Drive", tabBarIcon: icon("navigate-outline") }}
        />
        <Tabs.Screen
          name="earnings"
          options={{ title: "Earnings", tabBarIcon: icon("wallet-outline") }}
        />
      </Tabs.Protected>
      <Tabs.Screen
        name="account"
        options={{ title: "Account", tabBarIcon: icon("person-circle-outline") }}
      />
    </Tabs>
  );
};

export default AppLayout;
