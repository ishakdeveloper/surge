import { useTabBarSpace } from "@/components/app/tab-bar.js";
import type * as React from "react";
import { ScrollView, View } from "react-native";

/**
 * The rider's screen — the web's phone layout, which is where that one was
 * designed first: the map in a band across the top, and a white sheet rising
 * over its lower edge. The sheet's cards scroll; its footer — the one action
 * that moves the trip on — does not, so it is always under the thumb, just
 * above the floating tab bar.
 */
export const RideLayout = (props: {
  readonly map: React.ReactNode;
  /** The pinned action, or `null` when the sheet has none right now. */
  readonly footer: React.ReactNode;
  readonly children: React.ReactNode;
}) => {
  const bar = useTabBarSpace();
  return (
    <View className="flex-1 bg-background">
      <View className="h-[44%]">{props.map}</View>
      <View
        className="-mt-6 flex-1 rounded-t-3xl bg-card"
        style={{
          shadowColor: "#000000",
          shadowOpacity: 0.12,
          shadowRadius: 16,
          shadowOffset: { width: 0, height: -6 },
          elevation: 8,
        }}
      >
        <View className="mt-2.5 mb-1 h-1 w-10 self-center rounded-full bg-foreground/15" />
        <ScrollView
          className="flex-1"
          contentContainerClassName="gap-2 px-3 pt-1.5"
          contentContainerStyle={{ paddingBottom: props.footer === null ? bar + 16 : 16 }}
          keyboardShouldPersistTaps="handled"
          // A field the keyboard would cover scrolls above it instead: the
          // sheet is short, and onboarding's name field sits low in it.
          automaticallyAdjustKeyboardInsets
        >
          {props.children}
        </ScrollView>
        {props.footer !== null && (
          <View className="px-3 pt-2" style={{ paddingBottom: bar + 4 }}>{props.footer}</View>
        )}
      </View>
    </View>
  );
};
