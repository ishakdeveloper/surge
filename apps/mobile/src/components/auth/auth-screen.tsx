import { SurgeMark } from "@/components/app/surge-mark.js";
import { type MapMarker, type MapPoint, SurgeMap } from "@/components/map/surge-map.js";
import { Text } from "@/components/ui/text.js";
import type * as React from "react";
import { KeyboardAvoidingView, Platform, ScrollView, View } from "react-native";
import Animated, { FadeInLeft, FadeInRight, ReduceMotion } from "react-native-reanimated";
import { useSafeAreaInsets } from "react-native-safe-area-context";

const NO_MARKERS: ReadonlyArray<MapMarker> = [];
const NO_POINTS: ReadonlyArray<MapPoint> = [];

/**
 * The auth screens stand where the rider's screen does — the web's auth
 * layout on a phone: a band of the live map across the top, and a white sheet
 * rising over its lower edge. The first thing anyone sees is the city Surge
 * serves, not a form in a void.
 *
 * On the sheet, the web's `AuthCard`: the mark, the wordmark and the city,
 * a heading that says what this step is for, and the step itself, sliding in
 * from the side it moves towards. The heading stays put between steps, so the
 * eye does not have to find it again.
 */
export const AuthScreen = (props: {
  readonly title: string;
  readonly description: React.ReactNode;
  /** Which step is showing: a new one slides in. */
  readonly step: string;
  /** The side the step comes from: forward from the right, back from the left. */
  readonly direction: 1 | -1;
  readonly children: React.ReactNode;
}) => {
  const insets = useSafeAreaInsets();
  const slide = (props.direction === 1 ? FadeInRight : FadeInLeft)
    .duration(240)
    .reduceMotion(ReduceMotion.System);

  return (
    <KeyboardAvoidingView
      behavior={Platform.OS === "ios" ? "padding" : "height"}
      className="flex-1 bg-background"
    >
      {
        /* The city, as a backdrop: nothing on it is interactive here, so it
          takes no touches and is hidden from assistive tech. */
      }
      <View
        pointerEvents="none"
        accessibilityElementsHidden
        importantForAccessibility="no-hide-descendants"
        style={{ height: "30%" }}
      >
        <SurgeMap markers={NO_MARKERS} route={NO_POINTS} follow={NO_POINTS} />
      </View>

      {/* The sheet's edge shadow sits outside its clip, which iOS would cut off. */}
      <View
        className="-mt-6 flex-1 rounded-t-3xl bg-card"
        style={{
          shadowColor: "#000000",
          shadowOpacity: 0.18,
          shadowRadius: 15,
          shadowOffset: { width: 0, height: -10 },
          elevation: 8,
        }}
      >
        <View className="flex-1 overflow-hidden rounded-t-3xl">
          <ScrollView
            className="flex-1"
            contentContainerClassName="px-5 pt-7"
            contentContainerStyle={{ paddingBottom: insets.bottom + 40 }}
            keyboardShouldPersistTaps="handled"
            keyboardDismissMode="interactive"
          >
            <View className="w-full max-w-md gap-7 self-center">
              <View className="flex-row items-center gap-2.5">
                <SurgeMark size={36} />
                <Text className="text-xl font-semibold">Surge</Text>
                <View className="rounded-full bg-tile px-2.5 py-1">
                  <Text className="text-xs font-semibold text-muted-foreground">Amsterdam</Text>
                </View>
              </View>

              <View className="gap-2">
                <Text accessibilityRole="header" className="text-[28px] leading-8 font-semibold">
                  {props.title}
                </Text>
                <Text className="text-[15px] leading-[22px] text-muted-foreground">
                  {props.description}
                </Text>
              </View>

              <Animated.View key={props.step} entering={slide}>
                {props.children}
              </Animated.View>
            </View>
          </ScrollView>
        </View>
      </View>
    </KeyboardAvoidingView>
  );
};
