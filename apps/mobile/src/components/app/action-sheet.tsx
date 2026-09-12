import { PressableScale } from "@/components/motion/pressable-scale.js";
import { Text } from "@/components/ui/text.js";
import { colors } from "@/lib/theme.js";
import { cn } from "@/lib/utils.js";
import Ionicons from "@expo/vector-icons/Ionicons";
import type * as React from "react";
import { Modal, Pressable, View } from "react-native";
import Animated, {
  FadeIn,
  FadeOut,
  ReduceMotion,
  SlideInDown,
  SlideOutDown,
} from "react-native-reanimated";
import { useSafeAreaInsets } from "react-native-safe-area-context";

export interface SheetOption {
  readonly label: string;
  readonly icon: React.ComponentProps<typeof Ionicons>["name"];
  readonly onPress: () => void;
  /** A way out that cannot be taken back: said in red. */
  readonly danger?: boolean;
  /** For the end-to-end flows, where the option repeats the label behind it. */
  readonly testID?: string;
}

const rise = SlideInDown.springify().damping(22).stiffness(260).reduceMotion(ReduceMotion.System);
const sink = SlideOutDown.duration(180).reduceMotion(ReduceMotion.System);

/**
 * A short list of choices that rises from the bottom over a dimmed screen —
 * how a ride app asks "camera or library?" or "cancel this trip?" without
 * leaving the screen. A white card with rounded top, a title, the options as
 * rows, and Cancel apart below them; a tap on the dim closes it too.
 */
export const ActionSheet = (props: {
  readonly visible: boolean;
  readonly title: string;
  readonly message?: string;
  readonly options: ReadonlyArray<SheetOption>;
  readonly onClose: () => void;
}) => {
  const insets = useSafeAreaInsets();

  return (
    <Modal
      visible={props.visible}
      transparent
      animationType="none"
      statusBarTranslucent
      onRequestClose={props.onClose}
    >
      {props.visible && (
        <View className="flex-1 justify-end">
          <Animated.View
            entering={FadeIn.duration(180)}
            exiting={FadeOut.duration(160)}
            className="absolute inset-0 bg-black/40"
          >
            <Pressable
              accessibilityRole="button"
              accessibilityLabel="Close"
              className="flex-1"
              onPress={props.onClose}
            />
          </Animated.View>
          <Animated.View
            entering={rise}
            exiting={sink}
            accessibilityViewIsModal
            className="gap-2 px-3"
            style={{ paddingBottom: Math.max(insets.bottom, 12) }}
          >
            <View className="overflow-hidden rounded-3xl bg-card">
              <View className="gap-1 px-5 pt-5 pb-3">
                <Text accessibilityRole="header" className="text-[17px] font-semibold">
                  {props.title}
                </Text>
                {props.message !== undefined && (
                  <Text className="text-[15px] text-muted-foreground">{props.message}</Text>
                )}
              </View>
              {props.options.map((option) => (
                <PressableScale
                  key={option.label}
                  testID={option.testID}
                  scaleTo={0.99}
                  onPress={() => {
                    props.onClose();
                    option.onPress();
                  }}
                  className="flex-row items-center gap-3 px-5 py-4 active:bg-tile"
                >
                  <Ionicons
                    name={option.icon}
                    size={22}
                    color={option.danger === true ? "#c8312a" : colors.foreground}
                  />
                  <Text
                    className={cn(
                      "text-[16px] font-semibold",
                      option.danger === true && "text-destructive",
                    )}
                  >
                    {option.label}
                  </Text>
                </PressableScale>
              ))}
            </View>
            <PressableScale
              onPress={props.onClose}
              className="h-14 items-center justify-center rounded-full bg-card"
            >
              <Text className="text-[16px] font-semibold">Cancel</Text>
            </PressableScale>
          </Animated.View>
        </View>
      )}
    </Modal>
  );
};
