import { PressableScale } from "@/components/motion/pressable-scale.js";
import { Text } from "@/components/ui/text.js";
import { colors } from "@/lib/theme.js";
import { cn } from "@/lib/utils.js";
import Ionicons from "@expo/vector-icons/Ionicons";
import type { SignUpRole } from "@surge/common/iam/auth-schemas";
import { Option } from "effect";
import type * as React from "react";
import { View } from "react-native";
import Animated, { ReduceMotion, ZoomIn } from "react-native-reanimated";

const ROLES: ReadonlyArray<{
  readonly value: SignUpRole;
  readonly label: string;
  readonly description: string;
  readonly icon: React.ComponentProps<typeof Ionicons>["name"];
}> = [
  {
    value: "rider",
    label: "Ride",
    description: "Book trips across Amsterdam.",
    icon: "location-outline",
  },
  {
    value: "driver",
    label: "Drive",
    description: "Take trips while on shift.",
    icon: "car-outline",
  },
];

/**
 * The rider/driver choice — the web's `roleField` for the phone: two tiles
 * like the rider's class tiles, the chosen one yellow, each with its glyph in
 * a white disc and a radio mark that pops a check when chosen.
 *
 * Announced as radios with their checked state, the whole tile the target,
 * and one message for the group rather than one per option.
 */
export const roleField = (options: { readonly legend: string; }) =>
(props: {
  readonly field: {
    readonly value: SignUpRole;
    readonly onChange: (value: SignUpRole) => void;
    readonly onBlur: () => void;
    readonly error: Option.Option<string>;
    readonly isDirty: boolean;
  };
  readonly props: { readonly submitted?: boolean; };
}) => {
  const reveal = props.field.isDirty || props.props.submitted === true;
  const error = reveal ? props.field.error : Option.none();

  return (
    <View className="gap-2" accessibilityRole="radiogroup" accessibilityLabel={options.legend}>
      <Text className="text-[13px] font-semibold text-muted-foreground">{options.legend}</Text>
      <View className="flex-row gap-2">
        {ROLES.map((role) => {
          const checked = props.field.value === role.value;
          return (
            <PressableScale
              key={role.value}
              testID={`role-${role.value}`}
              accessibilityRole="radio"
              accessibilityState={{ checked }}
              accessibilityLabel={`${role.label}. ${role.description}`}
              feedback="select"
              scaleTo={0.98}
              onPress={() => {
                props.field.onChange(role.value);
                props.field.onBlur();
              }}
              className={cn(
                "flex-1 gap-2.5 rounded-2xl p-3.5",
                checked ? "bg-primary" : "bg-tile active:bg-tile-hover",
              )}
            >
              <View className="flex-row items-center justify-between">
                <View className="size-9 items-center justify-center rounded-full bg-card">
                  <Ionicons name={role.icon} size={16} color={colors.foreground} />
                </View>
                <View
                  className={cn(
                    "size-5 items-center justify-center rounded-full",
                    checked ? "bg-foreground" : "border-2 border-foreground/20",
                  )}
                >
                  {checked && (
                    <Animated.View
                      entering={ZoomIn.springify().damping(16).stiffness(600).reduceMotion(
                        ReduceMotion.System,
                      )}
                    >
                      <Ionicons name="checkmark" size={12} color={colors.primary} />
                    </Animated.View>
                  )}
                </View>
              </View>
              <View className="gap-0.5">
                <Text className="text-base font-semibold">{role.label}</Text>
                <Text className="text-[13px] leading-[18px] opacity-70">{role.description}</Text>
              </View>
            </PressableScale>
          );
        })}
      </View>
      {Option.isSome(error) && (
        <Text className="text-[13px] font-medium text-destructive">{error.value}</Text>
      )}
    </View>
  );
};
