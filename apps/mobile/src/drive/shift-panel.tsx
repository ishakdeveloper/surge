import { ActionError } from "@/components/app/errors.js";
import { PressableScale } from "@/components/motion/pressable-scale.js";
import { Sign, SignText } from "@/components/sign/sign.js";
import { Alert, AlertDescription } from "@/components/ui/alert.js";
import { Button } from "@/components/ui/button.js";
import { Text } from "@/components/ui/text.js";
import { haptic } from "@/lib/haptics.js";
import { colors } from "@/lib/theme.js";
import { cn } from "@/lib/utils.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import Ionicons from "@expo/vector-icons/Ionicons";
import { PositionUnavailable } from "@surge/client/Geolocation";
import { followAtom, followingAtom, shiftAtom } from "@surge/common/atom/driver-atoms";
import { connectionAtom } from "@surge/common/atom/realtime-atoms";
import { activeTripAtom } from "@surge/common/atom/trip-atoms";
import { statusFor } from "@surge/common/drive/driver-status";
import { formatPoint } from "@surge/common/lib/format";
import { DRIVER_SPOTS } from "@surge/common/ride/presets";
import { Cause, Option, Schema } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";
import * as React from "react";
import { ScrollView, View } from "react-native";
import Animated, {
  Easing,
  ReduceMotion,
  useAnimatedStyle,
  useSharedValue,
  withRepeat,
  withTiming,
} from "react-native-reanimated";

const isPositionUnavailable = Schema.is(PositionUnavailable);

/** What each refusal means for the person holding the phone. */
const positionMessage: Record<PositionUnavailable["reason"], string> = {
  Unsupported: "This device cannot report a position. Tap the map or choose a spot instead.",
  Denied: "Location permission was denied. Allow it in Settings, or tap the map instead.",
  Unavailable: "No position is available right now. Tap the map or choose a spot instead.",
  Timeout: "Finding your position took too long. Tap the map or choose a spot instead.",
};

/**
 * The shift, as the top of the driver's sheet: whether they are online — in
 * yellow when they are, with a ring breathing while trips are being looked
 * for — the one big switch, and where they are standing.
 *
 * The status sent to the matcher is shown as it is sent — `idle`, `offline`,
 * `enroute_pickup` — because it is derived, not chosen, and a driver wondering
 * why no offers arrive should be able to see what the system thinks they are.
 */
export const ShiftPanel = () => {
  const shift = useAtomValue(shiftAtom);
  const setShift = useAtomSet(shiftAtom);
  const connection = useAtomValue(connectionAtom);
  const following = useAtomValue(followingAtom);
  const setFollowing = useAtomSet(followingAtom);
  const tracking = useAtomValue(followAtom);
  const tripStatus = useAtomValue(
    activeTripAtom,
    (result) => Option.map(Option.flatten(AsyncResult.value(result)), (trip) => trip.status),
  );

  const connected = AsyncResult.isSuccess(connection) && connection.value === "Connected";
  const placed = Option.isSome(shift.position);
  const refusal = AsyncResult.isFailure(tracking)
    ? Option.filter(Cause.findErrorOption(tracking.cause), isPositionUnavailable)
    : Option.none();

  return (
    <View className="gap-3">
      <Sign tone={shift.online ? "direction" : "choice"} className="py-4">
        <Beacon searching={shift.online && connected} />
        <View className="min-w-0 flex-1 gap-0.5">
          <SignText accessibilityRole="header" className="text-xl leading-tight font-semibold">
            {shift.online ? "You're online" : "You're offline"}
          </SignText>
          <SignText className="text-sm opacity-80">
            {!connected
              ? "Connecting to Surge…"
              : shift.online
              ? "Looking for trips near you."
              : placed
              ? "Go online to get trip offers."
              : "Choose where you are, then go online."}
          </SignText>
        </View>
      </Sign>

      <Button
        // Going off shift is a service, not the next step, so the pill turns
        // ink — through the variant, which carries the white label with it. A
        // background swapped by class alone left an ink label on an ink pill.
        variant={shift.online ? "secondary" : "default"}
        className="h-14"
        disabled={!placed}
        feedback={shift.online ? "warning" : "success"}
        onPress={() => {
          setShift({ ...shift, online: !shift.online });
        }}
      >
        {shift.online ? "Go offline" : "Go online"}
      </Button>

      {Option.match(refusal, {
        onSome: (error) => (
          <Alert>
            <AlertDescription>{positionMessage[error.reason]}</AlertDescription>
          </Alert>
        ),
        onNone: () =>
          AsyncResult.isFailure(tracking) ? <ActionError cause={tracking.cause} /> : null,
      })}

      <View className="gap-2 pt-1">
        <Text
          accessibilityRole="header"
          className="px-1 text-[13px] font-semibold text-muted-foreground"
        >
          Where you are
        </Text>
        <ScrollView
          horizontal
          showsHorizontalScrollIndicator={false}
          className="-mx-3"
          contentContainerClassName="gap-2 px-3"
        >
          <PressableScale
            accessibilityRole="switch"
            accessibilityState={{ checked: following }}
            accessibilityLabel="Follow my GPS"
            feedback="select"
            onPress={() => {
              setFollowing(!following);
            }}
            className={cn(
              "h-10 flex-row items-center gap-1.5 rounded-full px-4",
              following ? "bg-secondary" : "bg-tile active:bg-tile-hover",
            )}
          >
            <Ionicons
              name={following ? "navigate" : "navigate-outline"}
              size={15}
              color={following ? colors.primary : colors.foreground}
            />
            <Text
              className={cn("text-sm font-semibold", following && "text-secondary-foreground")}
            >
              {following ? "Following GPS" : "Follow my GPS"}
            </Text>
          </PressableScale>
          {DRIVER_SPOTS.map((spot) => {
            const here = !following && Option.isSome(shift.position)
              && shift.position.value.lat === spot.position.lat
              && shift.position.value.lng === spot.position.lng;
            return (
              <PressableScale
                key={spot.label}
                accessibilityRole="radio"
                accessibilityState={{ checked: here }}
                feedback="select"
                onPress={() => {
                  setFollowing(false);
                  setShift({ ...shift, position: Option.some(spot.position) });
                }}
                className={cn(
                  "h-10 justify-center rounded-full px-4",
                  here ? "bg-primary" : "bg-tile active:bg-tile-hover",
                )}
              >
                <Text className="text-sm font-semibold">{spot.label}</Text>
              </PressableScale>
            );
          })}
        </ScrollView>
        <Text className="px-1 text-[13px] text-muted-foreground">
          Or tap the map where you are standing.
        </Text>
      </View>

      <View className="gap-1.5 rounded-2xl bg-tile px-4 py-3">
        <Fact label="Position">
          {Option.match(shift.position, { onNone: () => "-", onSome: formatPoint })}
        </Fact>
        <Fact label="Sent to the matcher">{statusFor(shift.online, tripStatus)}</Fact>
      </View>
    </View>
  );
};

const Fact = (props: { readonly label: string; readonly children: string; }) => (
  <View className="flex-row items-center justify-between gap-3">
    <Text className="text-[13px] text-muted-foreground">{props.label}</Text>
    <Text selectable className="font-mono text-xs">{props.children}</Text>
  </View>
);

/**
 * The shift's mark: a car in a white well, and while trips are being looked
 * for a ring that swells out of it and fades, like a signal going out. Still
 * when the system asks for reduced motion.
 */
const Beacon = (props: { readonly searching: boolean; }) => {
  const pulse = useSharedValue(0);
  React.useEffect(() => {
    if (props.searching) {
      haptic.tap();
      pulse.value = withRepeat(
        withTiming(1, {
          duration: 1600,
          easing: Easing.out(Easing.quad),
          reduceMotion: ReduceMotion.System,
        }),
        -1,
      );
    } else {
      pulse.value = withTiming(0, { duration: 160 });
    }
  }, [props.searching, pulse]);
  const ring = useAnimatedStyle(() => ({
    opacity: props.searching ? 0.55 * (1 - pulse.value) : 0,
    transform: [{ scale: 1 + pulse.value * 0.9 }],
  }));

  return (
    <View
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
      className="size-10 items-center justify-center"
    >
      <Animated.View className="absolute size-10 rounded-full bg-card" style={ring} />
      <View className="size-10 items-center justify-center rounded-full bg-card">
        <Ionicons name="car-sport" size={20} color={colors.foreground} />
      </View>
    </View>
  );
};
