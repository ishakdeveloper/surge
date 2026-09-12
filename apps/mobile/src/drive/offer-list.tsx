import { ActionError } from "@/components/app/errors.js";
import { useTabBarSpace } from "@/components/app/tab-bar.js";
import type { MapPoint } from "@/components/map/surge-map.js";
import { PressableScale } from "@/components/motion/pressable-scale.js";
import { Text } from "@/components/ui/text.js";
import { haptic } from "@/lib/haptics.js";
import { colors } from "@/lib/theme.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import Ionicons from "@expo/vector-icons/Ionicons";
import { answerOffer } from "@surge/common/atom/realtime-atoms";
import { secondsLeft } from "@surge/common/drive/driver-status";
import { formatDistance, formatPoint } from "@surge/common/lib/format";
import type { Offer } from "@surge/domain/realtime/Wire";
import { Option } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";
import * as React from "react";
import { AccessibilityInfo, ActivityIndicator, StyleSheet, View } from "react-native";
import Animated, {
  Easing,
  ReduceMotion,
  SlideInDown,
  SlideOutDown,
  useAnimatedStyle,
  useSharedValue,
  withTiming,
} from "react-native-reanimated";

/** As the crow flies — enough to say how far a pickup is, not how long it takes. */
const metersBetween = (a: MapPoint, b: MapPoint): number => {
  const radians = (degrees: number) => (degrees * Math.PI) / 180;
  const dLat = radians(b.lat - a.lat);
  const dLng = radians(b.lng - a.lng);
  const h = Math.sin(dLat / 2) ** 2
    + Math.cos(radians(a.lat)) * Math.cos(radians(b.lat)) * Math.sin(dLng / 2) ** 2;
  return 2 * 6_371_000 * Math.asin(Math.sqrt(h));
};

const rise = SlideInDown.springify().damping(20).stiffness(220).reduceMotion(ReduceMotion.System);
const sink = SlideOutDown.duration(200).reduceMotion(ReduceMotion.System);

/**
 * A trip on offer, put in front of the driver the way a ride app does it: a
 * black card rising over the sheet with how far the pickup is, a yellow bar
 * running down the time left, and Accept — the next step, in yellow — beside
 * Decline. One at a time, the oldest first; the card says how many more wait.
 *
 * Announced and felt, because an offer arriving is the one event a driver
 * cannot afford to miss, and it expires.
 */
export const OfferPopup = (props: {
  readonly offers: ReadonlyArray<Offer>;
  readonly now: number;
  readonly position: Option.Option<MapPoint>;
  readonly online: boolean;
}) => {
  const bar = useTabBarSpace();
  const offer = props.offers[0];
  // When each offer was first seen, so its bar starts full whatever its expiry.
  const seen = React.useRef(new Map<string, number>());
  if (offer !== undefined && !seen.current.has(offer.tripId)) {
    seen.current.set(offer.tripId, props.now);
  }

  // Said and felt once per offer, as it arrives. The countdown is on screen,
  // not read out every second.
  React.useEffect(() => {
    if (offer === undefined) return;
    haptic.warning();
    AccessibilityInfo.announceForAccessibility("New trip offer. Accept or decline.");
    // Keyed on the trip, not the offer object, which is new on every tick.
    // oxlint-disable-next-line react-hooks/exhaustive-deps
  }, [offer?.tripId]);

  return (
    <View pointerEvents="box-none" style={StyleSheet.absoluteFill}>
      {offer !== undefined && (
        <Animated.View
          key={offer.tripId}
          entering={rise}
          exiting={sink}
          className="absolute right-3 left-3"
          style={{ bottom: bar + 4 }}
        >
          <OfferCard
            offer={offer}
            now={props.now}
            firstSeen={seen.current.get(offer.tripId) ?? props.now}
            more={props.offers.length - 1}
            position={props.position}
          />
        </Animated.View>
      )}
    </View>
  );
};

const OfferCard = (props: {
  readonly offer: Offer;
  readonly now: number;
  readonly firstSeen: number;
  readonly more: number;
  readonly position: Option.Option<MapPoint>;
}) => {
  const answering = useAtomValue(answerOffer);
  const answer = useAtomSet(answerOffer);
  const { offer } = props;
  const pickup = { lat: offer.pickupLat, lng: offer.pickupLng };
  const away = Option.map(props.position, (here) => metersBetween(here, pickup));
  const left = secondsLeft(offer, props.now);

  // The bar runs down once, smoothly, from what was left when it appeared.
  const remaining = useSharedValue(1);
  React.useEffect(() => {
    const total = Math.max(1, offer.expiresAtMs - props.firstSeen);
    // oxlint-disable-next-line effecttsgo/global-date
    const now = Date.now();
    remaining.value = Math.max(0, (offer.expiresAtMs - now) / total);
    remaining.value = withTiming(0, {
      duration: Math.max(0, offer.expiresAtMs - now),
      easing: Easing.linear,
      reduceMotion: ReduceMotion.Never,
    });
  }, [offer.expiresAtMs, props.firstSeen, remaining]);
  const running = useAnimatedStyle(() => ({ width: `${remaining.value * 100}%` }));

  return (
    <View accessibilityViewIsModal className="overflow-hidden rounded-3xl bg-secondary">
      <View
        style={{
          shadowColor: "#000000",
          shadowOpacity: 0.3,
          shadowRadius: 24,
          shadowOffset: { width: 0, height: 12 },
          elevation: 16,
        }}
      >
        <View className="h-1.5 bg-white/10">
          <Animated.View className="h-full bg-primary" style={running} />
        </View>
        <View className="gap-4 p-5">
          <View className="flex-row items-start gap-3">
            <View className="min-w-0 flex-1 gap-1">
              <Text className="text-[13px] font-semibold text-white/70">
                New trip{props.more > 0 ? ` · ${props.more} more waiting` : ""}
              </Text>
              <Text className="text-2xl leading-[30px] font-semibold text-white">
                {Option.match(away, {
                  onNone: () => "Pickup nearby",
                  onSome: (meters) => `${formatDistance(meters)} away`,
                })}
              </Text>
              <View className="flex-row items-center gap-1.5">
                <Ionicons name="location" size={13} color={colors.primary} />
                <Text className="font-mono text-xs text-white/70">{formatPoint(pickup)}</Text>
              </View>
            </View>
            <View
              accessibilityElementsHidden
              importantForAccessibility="no-hide-descendants"
              className="size-14 items-center justify-center rounded-full border-[3px] border-primary"
            >
              <Text className="text-xl font-semibold text-white tabular-nums">{left}</Text>
            </View>
          </View>

          {AsyncResult.isFailure(answering) && !answering.waiting && (
            <ActionError cause={answering.cause} />
          )}

          <View className="flex-row gap-2">
            <PressableScale
              accessibilityState={{ disabled: answering.waiting }}
              disabled={answering.waiting}
              feedback="select"
              onPress={() => {
                answer({ offer, accepted: false });
              }}
              className="h-14 flex-1 items-center justify-center rounded-full bg-white/10 active:bg-white/15"
            >
              <Text className="text-[15px] font-semibold text-white">Decline</Text>
            </PressableScale>
            <PressableScale
              accessibilityState={{ disabled: answering.waiting, busy: answering.waiting }}
              disabled={answering.waiting}
              feedback="success"
              onPress={() => {
                answer({ offer, accepted: true });
              }}
              className="h-14 flex-[2] flex-row items-center justify-center gap-2 rounded-full bg-primary active:bg-[#f0c400]"
            >
              {answering.waiting && <ActivityIndicator color={colors.foreground} />}
              <Text className="text-[17px] font-semibold text-primary-foreground">Accept</Text>
            </PressableScale>
          </View>
        </View>
      </View>
    </View>
  );
};
