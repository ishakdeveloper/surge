import { Text } from "@/components/ui/text.js";
import { haptic } from "@/lib/haptics.js";
import { cn } from "@/lib/utils.js";
import * as React from "react";
import { StyleSheet, TextInput, View } from "react-native";
import Animated, {
  ReduceMotion,
  useAnimatedStyle,
  useSharedValue,
  withRepeat,
  withSequence,
  withTiming,
  ZoomIn,
} from "react-native-reanimated";

/**
 * A one-time code as one box per digit — the web's `OtpInput` for the phone.
 *
 * One real field lies invisibly over the boxes, so everything a phone does
 * with a code field still works: the number pad, iOS offering the code from
 * the message that just arrived, Android's autofill, a paste. The boxes only
 * draw it: each digit pops in as it lands, the box waiting for the next one
 * is ringed in ink with a blinking caret, and the whole row shakes red when
 * the code is refused and rings green when it is taken.
 */
export const OtpInput = (props: {
  readonly value: string;
  readonly onChange: (value: string) => void;
  /** Called with the whole code once the last digit is typed. */
  readonly onComplete?: ((value: string) => void) | undefined;
  readonly onBlur?: (() => void) | undefined;
  readonly length?: number;
  readonly autoFocus?: boolean;
  readonly disabled?: boolean;
  readonly status?: "idle" | "error" | "success";
  readonly testID?: string;
  readonly accessibilityLabel: string;
  readonly accessibilityHint?: string | undefined;
}) => {
  const length = props.length ?? 6;
  const status = props.status ?? "idle";
  const digits = props.value.replace(/\D/g, "").slice(0, length);
  const [focused, setFocused] = React.useState(false);
  const next = Math.min(digits.length, length - 1);

  const shake = useSharedValue(0);
  React.useEffect(() => {
    if (status === "error") {
      haptic.warning();
      shake.value = withSequence(
        withTiming(-8, { duration: 50, reduceMotion: ReduceMotion.System }),
        withRepeat(withTiming(8, { duration: 90, reduceMotion: ReduceMotion.System }), 3, true),
        withTiming(0, { duration: 50, reduceMotion: ReduceMotion.System }),
      );
    }
    if (status === "success") haptic.success();
  }, [status, shake]);
  const shaking = useAnimatedStyle(() => ({ transform: [{ translateX: shake.value }] }));

  return (
    <View>
      <Animated.View
        accessibilityElementsHidden
        importantForAccessibility="no-hide-descendants"
        className="flex-row gap-2"
        style={shaking}
      >
        {Array.from({ length }, (_, index) => {
          const digit = digits[index] ?? "";
          const here = focused && index === next && digits.length < length;
          return (
            <View
              key={index}
              className={cn(
                "h-14 flex-1 items-center justify-center rounded-md border-2",
                here ? "bg-card" : "bg-tile",
                status === "error"
                  ? "border-destructive"
                  : status === "success"
                  ? "border-success"
                  : here
                  ? "border-foreground"
                  : "border-transparent",
                props.disabled === true && "opacity-50",
              )}
            >
              {digit !== ""
                ? (
                  <Animated.View
                    key={`${index}-${digit}`}
                    entering={ZoomIn.springify().damping(14).stiffness(420).reduceMotion(
                      ReduceMotion.System,
                    )}
                  >
                    <Text className="text-2xl font-semibold tabular-nums">{digit}</Text>
                  </Animated.View>
                )
                : here
                ? <Caret />
                : null}
            </View>
          );
        })}
      </Animated.View>
      <TextInput
        testID={props.testID}
        accessibilityLabel={props.accessibilityLabel}
        accessibilityHint={props.accessibilityHint}
        value={digits}
        onChangeText={(text) => {
          const clean = text.replace(/\D/g, "").slice(0, length);
          props.onChange(clean);
          if (clean.length === length && clean !== digits) props.onComplete?.(clean);
        }}
        onFocus={() => {
          setFocused(true);
        }}
        onBlur={() => {
          setFocused(false);
          props.onBlur?.();
        }}
        editable={props.disabled !== true}
        autoFocus={props.autoFocus}
        maxLength={length}
        keyboardType="number-pad"
        textContentType="oneTimeCode"
        autoComplete="one-time-code"
        caretHidden
        contextMenuHidden={false}
        selectionColor="transparent"
        // Over the boxes, so a tap anywhere on them focuses it, and all but
        // invisible — fully transparent fields stop receiving touches on
        // some Android versions.
        style={[StyleSheet.absoluteFill, { opacity: 0.011, color: "transparent" }]}
      />
    </View>
  );
};

/** The caret in the box waiting for a digit, blinking; steady under reduced motion. */
const Caret = () => {
  const opacity = useSharedValue(1);
  React.useEffect(() => {
    opacity.value = withRepeat(
      withSequence(
        withTiming(0, { duration: 450, reduceMotion: ReduceMotion.System }),
        withTiming(1, { duration: 450, reduceMotion: ReduceMotion.System }),
      ),
      -1,
    );
  }, [opacity]);
  const blinking = useAnimatedStyle(() => ({ opacity: opacity.value }));
  return <Animated.View className="h-6 w-0.5 rounded-full bg-foreground" style={blinking} />;
};
