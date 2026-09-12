import { sessionAtom } from "@/atom/session-atoms.js";
import { TabBar, TabBarScope } from "@/components/app/tab-bar.js";
import { colors } from "@/lib/theme.js";
import { useAtomValue } from "@effect/atom-react";
import { AsyncResult } from "effect/unstable/reactivity";
import { Tabs } from "expo-router";

/**
 * The signed-in app's tabs: one surface per role, the conversations, and the
 * account — under a floating bar rather than a docked one, so the map runs to
 * the bottom of the screen.
 *
 * A role sees only its own tabs. The trip list is scoped by whoever holds the
 * token, so a driver on the Ride tab would see the trips they drove presented
 * as rides they booked — hiding the tab is the honest version of the web's
 * "this page is for riders" notice. `ops` has neither: the console and the
 * support desk are web surfaces.
 *
 * No headers: each screen sets its own large title, as the rider's sheet does.
 */
const TabsLayout = () => {
  const role = useAtomValue(
    sessionAtom,
    (session) => AsyncResult.isSuccess(session) ? session.value.role : undefined,
  );

  return (
    <TabBarScope>
      <Tabs
        tabBar={(props) => <TabBar {...props} />}
        screenOptions={{
          headerShown: false,
          sceneStyle: { backgroundColor: colors.background },
          animation: "shift",
        }}
      >
        <Tabs.Screen name="index" options={{ href: null }} />
        <Tabs.Protected guard={role === "rider"}>
          <Tabs.Screen name="ride" options={{ title: "Ride" }} />
        </Tabs.Protected>
        <Tabs.Protected guard={role === "driver"}>
          <Tabs.Screen name="drive" options={{ title: "Drive" }} />
          <Tabs.Screen name="earnings" options={{ title: "Earnings" }} />
        </Tabs.Protected>
        <Tabs.Protected guard={role === "rider" || role === "driver"}>
          <Tabs.Screen name="messages" options={{ title: "Messages" }} />
        </Tabs.Protected>
        <Tabs.Screen name="account" options={{ title: "Account" }} />
      </Tabs>
    </TabBarScope>
  );
};

export default TabsLayout;
