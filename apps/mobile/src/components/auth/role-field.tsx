import { Text } from "@/components/ui/text.js";
import { cn } from "@/lib/utils.js";
import type { SignUpRole } from "@surge/common/iam/auth-schemas";
import { Option } from "effect";
import { Pressable, View } from "react-native";

const ROLES: ReadonlyArray<
  { readonly value: SignUpRole; readonly label: string; readonly description: string; }
> = [
  { value: "rider", label: "Rider", description: "Book trips." },
  { value: "driver", label: "Driver", description: "Get offered them." },
];

/**
 * The rider/driver choice, as the web's radio group: one message for the
 * group, the whole card the target, and announced as radios with their
 * checked state so a screen reader says which one is chosen.
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
      <Text className="font-medium">{options.legend}</Text>
      <View className="flex-row gap-2">
        {ROLES.map((role) => {
          const checked = props.field.value === role.value;
          return (
            <Pressable
              key={role.value}
              testID={`role-${role.value}`}
              accessibilityRole="radio"
              accessibilityState={{ checked }}
              onPress={() => {
                props.field.onChange(role.value);
                props.field.onBlur();
              }}
              className={cn(
                "flex-1 gap-0.5 rounded-lg border px-3 py-2.5",
                checked ? "border-primary" : "border-border",
              )}
            >
              <Text className="font-medium">{role.label}</Text>
              <Text className="text-xs text-muted-foreground">{role.description}</Text>
            </Pressable>
          );
        })}
      </View>
      {Option.isSome(error) && <Text className="text-xs text-destructive">{error.value}</Text>}
    </View>
  );
};
