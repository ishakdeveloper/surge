import type * as React from "react";
import { ScrollView, View } from "react-native";

/**
 * The rider's screen — the web's phone layout, which is where that one was
 * designed first: the map in a band across the top, and a white sheet rising
 * over its lower edge. The sheet's cards scroll; its footer — the one action
 * that moves the trip on — does not, so it is always under the thumb.
 */
export const RideLayout = (props: {
  readonly map: React.ReactNode;
  /** The pinned action, or `null` when the sheet has none right now. */
  readonly footer: React.ReactNode;
  readonly children: React.ReactNode;
}) => (
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
        contentContainerClassName="gap-2 px-3 pt-1.5 pb-4"
        keyboardShouldPersistTaps="handled"
      >
        {props.children}
      </ScrollView>
      {props.footer !== null && (
        <View className="border-t border-border/70 px-3 pt-2.5 pb-3">{props.footer}</View>
      )}
    </View>
  </View>
);
