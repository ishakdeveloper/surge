import { PressableScale } from "@/components/motion/pressable-scale.js";
import { Text } from "@/components/ui/text.js";
import { colors } from "@/lib/theme.js";
import { cn } from "@/lib/utils.js";
import { useAtomMount, useAtomValue } from "@effect/atom-react";
import Ionicons from "@expo/vector-icons/Ionicons";
import { chatPushesAtom, conversationsAtom } from "@surge/common/atom/chat-atoms";
import { AsyncResult } from "effect/unstable/reactivity";
import type { Tabs } from "expo-router";
import * as React from "react";
import { Keyboard, Platform, View } from "react-native";
import Animated, {
  ReduceMotion,
  useAnimatedStyle,
  useSharedValue,
  withSpring,
  ZoomIn,
} from "react-native-reanimated";
import { useSafeAreaInsets } from "react-native-safe-area-context";

type TabBarProps = Parameters<NonNullable<React.ComponentProps<typeof Tabs>["tabBar"]>>[0];
type IconName = React.ComponentProps<typeof Ionicons>["name"];

/** Every tab the bar knows how to draw, filled when chosen and outlined when not. */
const TABS = {
  ride: { label: "Ride", on: "car", off: "car-outline" },
  drive: { label: "Drive", on: "navigate", off: "navigate-outline" },
  earnings: { label: "Earnings", on: "wallet", off: "wallet-outline" },
  messages: { label: "Messages", on: "chatbubbles", off: "chatbubbles-outline" },
  account: { label: "Account", on: "person-circle", off: "person-circle-outline" },
} as const satisfies Record<string, { label: string; on: IconName; off: IconName; }>;

type TabName = keyof typeof TABS;
const isTab = (name: string): name is TabName => Object.hasOwn(TABS, name);

const BAR_HEIGHT = 64;
const INSET = 6;
const SLIDE = {
  damping: 20,
  stiffness: 260,
  mass: 0.7,
  reduceMotion: ReduceMotion.System,
} as const;

const InTabs = React.createContext(false);

/** Wraps the tabs, so a screen inside them knows to leave room for the bar. */
export const TabBarScope = (props: { readonly children: React.ReactNode; }) => (
  <InTabs.Provider value>{props.children}</InTabs.Provider>
);

/** How much of the bottom of the screen the floating bar covers; none outside the tabs. */
export const useTabBarSpace = (): number => {
  const inTabs = React.useContext(InTabs);
  const insets = useSafeAreaInsets();
  return inTabs ? BAR_HEIGHT + Math.max(insets.bottom, 12) + 12 : 0;
};

/**
 * The navigation, floating: a black pill above the bottom edge — navigation is
 * a service — with the tab you are on picked out by a yellow pill that slides
 * to it. Messages carries its unread count, so a driver's reply is seen from
 * any tab.
 *
 * On Android, whose keyboard pushes the window up, the bar steps aside while
 * the keyboard is open; on iOS the keyboard simply covers it.
 */
export const TabBar = ({ state, navigation, descriptors }: TabBarProps) => {
  const insets = useSafeAreaInsets();
  const keyboard = useKeyboardOpen();
  const [width, setWidth] = React.useState(0);
  const routes = state.routes.filter((route) => isTab(route.name));
  const current = state.routes[state.index];
  const active = routes.findIndex((route) => route.key === current?.key);
  const slot = width > 0 && routes.length > 0 ? (width - INSET * 2) / routes.length : 0;

  const x = useSharedValue(0);
  const placed = React.useRef(false);
  React.useEffect(() => {
    if (slot === 0 || active < 0) return;
    // The first placement jumps; only a change of tab slides.
    if (placed.current) {
      x.value = withSpring(active * slot, SLIDE);
    } else {
      x.value = active * slot;
      placed.current = true;
    }
  }, [active, slot, x]);
  const indicator = useAnimatedStyle(() => ({ transform: [{ translateX: x.value }] }));

  if (keyboard || routes.length < 2) return null;

  return (
    <View
      pointerEvents="box-none"
      className="absolute right-0 left-0 px-4"
      style={{ bottom: Math.max(insets.bottom, 12) }}
    >
      <View
        accessibilityRole="tablist"
        onLayout={(event) => {
          setWidth(event.nativeEvent.layout.width);
        }}
        className="flex-row rounded-full bg-secondary"
        style={{
          height: BAR_HEIGHT,
          padding: INSET,
          shadowColor: "#000000",
          shadowOpacity: 0.22,
          shadowRadius: 18,
          shadowOffset: { width: 0, height: 8 },
          elevation: 12,
        }}
      >
        {slot > 0 && active >= 0 && (
          <Animated.View
            pointerEvents="none"
            className="absolute rounded-full bg-primary"
            style={[{ top: INSET, bottom: INSET, left: INSET, width: slot }, indicator]}
          />
        )}
        {routes.map((route) => {
          if (!isTab(route.name)) return null;
          const tab = TABS[route.name];
          const focused = route.key === current?.key;
          const ink = focused ? colors.foreground : "rgba(255, 255, 255, 0.72)";
          return (
            <PressableScale
              key={route.key}
              accessibilityRole="tab"
              accessibilityState={{ selected: focused }}
              accessibilityLabel={descriptors[route.key]?.options.tabBarAccessibilityLabel
                ?? tab.label}
              feedback={focused ? "none" : "select"}
              scaleTo={0.92}
              onPress={() => {
                const event = navigation.emit({
                  type: "tabPress",
                  target: route.key,
                  canPreventDefault: true,
                });
                if (!focused && !event.defaultPrevented) {
                  navigation.navigate(route.name, route.params);
                }
              }}
              onLongPress={() => {
                navigation.emit({ type: "tabLongPress", target: route.key });
              }}
              className="flex-1 items-center justify-center gap-0.5"
            >
              <View>
                <Ionicons name={focused ? tab.on : tab.off} size={22} color={ink} />
                {route.name === "messages" && <UnreadBadge focused={focused} />}
              </View>
              <Text className="text-[11px] font-semibold" style={{ color: ink }}>{tab.label}</Text>
            </PressableScale>
          );
        })}
      </View>
    </View>
  );
};

/**
 * What is unread across every conversation, riding on the Messages icon.
 * Hidden from assistive tech: the Messages tab's own screen says it in words.
 */
const UnreadBadge = (props: { readonly focused: boolean; }) => {
  useAtomMount(chatPushesAtom);
  const unread = useAtomValue(
    conversationsAtom,
    (result) =>
      AsyncResult.isSuccess(result)
        ? result.value.reduce((sum, conversation) => sum + conversation.unread, 0)
        : 0,
  );
  if (unread === 0) return null;
  return (
    <Animated.View
      key={unread}
      entering={ZoomIn.springify().damping(14).reduceMotion(ReduceMotion.System)}
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
      className={cn(
        "absolute -top-1.5 -right-3 h-[18px] min-w-[18px] items-center justify-center rounded-full px-1",
        props.focused ? "bg-secondary" : "bg-primary",
      )}
    >
      <Text
        className={cn(
          "text-[10px] font-semibold tabular-nums",
          props.focused ? "text-primary" : "text-primary-foreground",
        )}
      >
        {unread > 99 ? "99+" : unread}
      </Text>
    </Animated.View>
  );
};

/** Whether the keyboard is open, on Android only — where it would lift the bar with it. */
const useKeyboardOpen = (): boolean => {
  const [open, setOpen] = React.useState(false);
  React.useEffect(() => {
    if (Platform.OS !== "android") return;
    const shown = Keyboard.addListener("keyboardDidShow", () => {
      setOpen(true);
    });
    const hidden = Keyboard.addListener("keyboardDidHide", () => {
      setOpen(false);
    });
    return () => {
      shown.remove();
      hidden.remove();
    };
  }, []);
  return open;
};
