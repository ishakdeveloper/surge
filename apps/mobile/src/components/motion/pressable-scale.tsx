import { type Haptic, haptic } from "@/lib/haptics.js";
import type * as React from "react";
import { Pressable } from "react-native";
import Animated, {
  ReduceMotion,
  useAnimatedStyle,
  useSharedValue,
  withSpring,
} from "react-native-reanimated";

// No `cssInterop` of its own: NativeWind's babel plugin sends Reanimated's
// `createElement` through its interop too, so `className` reaches the
// Pressable inside and is styled there. Registering this wrapper as well
// resolved the classes a layer early, and Reanimated dropped them.
const AnimatedPressable = Animated.createAnimatedComponent(Pressable);

/** Quick to settle and a touch springy: a press, not a bounce. */
const SPRING = {
  damping: 18,
  stiffness: 420,
  mass: 0.6,
  reduceMotion: ReduceMotion.System,
} as const;

/**
 * A control that settles under the thumb — a small spring down on press, back
 * on release — and taps back, the way the buttons in a ride app answer. The
 * spring is skipped when the system asks for reduced motion; the tap is not,
 * since it is not motion.
 */
export const PressableScale = ({
  onPressIn,
  onPressOut,
  onPress,
  feedback = "tap",
  scaleTo = 0.97,
  style,
  ...props
}: Omit<React.ComponentProps<typeof Pressable>, "ref"> & {
  readonly className?: string;
  /** What the phone gives back on a press, or nothing. */
  readonly feedback?: Haptic | "none";
  /** How far it settles: 0.97 for a button, less for a large card. */
  readonly scaleTo?: number;
}) => {
  const scale = useSharedValue(1);
  const settled = useAnimatedStyle(() => ({ transform: [{ scale: scale.value }] }));

  return (
    <AnimatedPressable
      accessibilityRole="button"
      {...props}
      style={[settled, style]}
      onPressIn={(event) => {
        scale.value = withSpring(scaleTo, SPRING);
        onPressIn?.(event);
      }}
      onPressOut={(event) => {
        scale.value = withSpring(1, SPRING);
        onPressOut?.(event);
      }}
      onPress={(event) => {
        if (feedback !== "none") haptic[feedback]();
        onPress?.(event);
      }}
    />
  );
};
