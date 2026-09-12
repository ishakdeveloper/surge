import { PressableScale } from "@/components/motion/pressable-scale.js";
import { Text } from "@/components/ui/text.js";
import { colors } from "@/lib/theme.js";
import Ionicons from "@expo/vector-icons/Ionicons";
import type * as React from "react";
import { View } from "react-native";

/**
 * The top of a sheet: a grabber that says it can be swiped away, the title
 * and what it is doing, and a round close button for anyone who would rather
 * tap than swipe.
 */
export const SheetHeader = (props: {
  readonly title: React.ReactNode;
  readonly subtitle?: React.ReactNode;
  /** A face or a glyph before the title. */
  readonly leading?: React.ReactNode;
  /** More actions, between the title and close. */
  readonly trailing?: React.ReactNode;
  readonly onClose: () => void;
}) => (
  <View className="gap-1 pb-2">
    <View className="mt-2 h-1 w-10 self-center rounded-full bg-foreground/15" />
    <View className="flex-row items-center gap-3 px-4 pt-2">
      {props.leading}
      <View className="min-w-0 flex-1">
        <Text
          accessibilityRole="header"
          numberOfLines={1}
          className="text-lg leading-tight font-semibold"
        >
          {props.title}
        </Text>
        {props.subtitle !== undefined && (
          <Text numberOfLines={1} className="text-[13px] text-muted-foreground">
            {props.subtitle}
          </Text>
        )}
      </View>
      {props.trailing}
      <PressableScale
        accessibilityLabel="Close"
        hitSlop={8}
        scaleTo={0.9}
        onPress={props.onClose}
        className="size-9 items-center justify-center rounded-full bg-tile active:bg-tile-hover"
      >
        <Ionicons name="close" size={20} color={colors.foreground} />
      </PressableScale>
    </View>
  </View>
);

/** The round tile a sheet's extra actions sit in, beside close. */
export const SheetButton = (props: {
  readonly icon: React.ComponentProps<typeof Ionicons>["name"];
  readonly label: string;
  readonly onPress: () => void;
}) => (
  <PressableScale
    accessibilityLabel={props.label}
    hitSlop={8}
    scaleTo={0.9}
    onPress={props.onPress}
    className="size-9 items-center justify-center rounded-full bg-tile active:bg-tile-hover"
  >
    <Ionicons name={props.icon} size={20} color={colors.foreground} />
  </PressableScale>
);
