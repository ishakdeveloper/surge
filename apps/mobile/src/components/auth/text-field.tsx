import { Input } from "@/components/ui/input.js";
import { Text } from "@/components/ui/text.js";
import { Option } from "effect";
import type * as React from "react";
import type { TextInput } from "react-native";
import { View } from "react-native";
import Animated, { FadeIn, ReduceMotion } from "react-native-reanimated";

type Kind = "email" | "phone" | "code";

const KEYBOARD: Record<Kind, React.ComponentProps<typeof TextInput>["keyboardType"]> = {
  email: "email-address",
  phone: "phone-pad",
  code: "number-pad",
};

/**
 * What iOS offers to fill in: an address from contacts, the phone's own
 * number, and — for `oneTimeCode` — the code from the message that just
 * arrived, straight off the keyboard.
 */
const CONTENT: Record<Kind, React.ComponentProps<typeof TextInput>["textContentType"]> = {
  email: "emailAddress",
  phone: "telephoneNumber",
  code: "oneTimeCode",
};

const AUTOCOMPLETE: Record<Kind, React.ComponentProps<typeof TextInput>["autoComplete"]> = {
  email: "email",
  phone: "tel",
  code: "one-time-code",
};

const PLACEHOLDER: Record<Kind, string> = {
  email: "you@example.com",
  phone: "+31 6 12345678",
  code: "123456",
};

/**
 * One effect-form field, as the web's `textField` renders it: a small grey
 * label over a grey field, and an error that stays hidden until the person has
 * typed — or `submitted` reveals it once a submit has been attempted, so an
 * empty form marks what is missing.
 */
export const textField = (options: { readonly label: string; readonly kind: Kind; }) =>
(props: {
  readonly field: {
    readonly value: string;
    readonly onChange: (value: string) => void;
    readonly onBlur: () => void;
    readonly error: Option.Option<string>;
    readonly isDirty: boolean;
  };
  readonly props: { readonly submitted?: boolean; };
}) => {
  const reveal = props.field.isDirty || props.props.submitted === true;
  const error = reveal ? props.field.error : Option.none();

  return (
    <View className="gap-2">
      <Text className="text-[13px] font-semibold text-muted-foreground">{options.label}</Text>
      <Input
        testID={`field-${options.kind}`}
        accessibilityLabel={options.label}
        accessibilityHint={Option.getOrUndefined(error)}
        value={props.field.value}
        onChangeText={props.field.onChange}
        onBlur={props.field.onBlur}
        placeholder={PLACEHOLDER[options.kind]}
        keyboardType={KEYBOARD[options.kind]}
        textContentType={CONTENT[options.kind]}
        autoComplete={AUTOCOMPLETE[options.kind]}
        autoCapitalize="none"
        autoCorrect={false}
        invalid={Option.isSome(error)}
      />
      {Option.isSome(error) && (
        <Animated.View entering={FadeIn.duration(160).reduceMotion(ReduceMotion.System)}>
          <Text className="text-[13px] font-medium text-destructive">{error.value}</Text>
        </Animated.View>
      )}
    </View>
  );
};
