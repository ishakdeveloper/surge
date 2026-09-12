import { colors } from "@/lib/theme.js";
import { cn } from "@/lib/utils.js";
import * as React from "react";
import { TextInput } from "react-native";

/**
 * A field, as the web's inputs are drawn: a grey tile with no border at rest,
 * that lifts to white with an ink ring while it has focus — tone, not lines.
 * `invalid` rings it in the refused red instead.
 */
export const Input = (
  { className, invalid, onFocus, onBlur, ...props }: React.ComponentProps<typeof TextInput> & {
    readonly invalid?: boolean;
  },
) => {
  const [focused, setFocused] = React.useState(false);

  return (
    <TextInput
      placeholderTextColor={colors.mutedForeground}
      selectionColor={colors.foreground}
      className={cn(
        "h-12 rounded-md border-2 px-4 text-[15px] text-foreground",
        focused ? "border-foreground bg-card" : "border-transparent bg-tile",
        invalid === true && "border-destructive",
        className,
      )}
      onFocus={(event) => {
        setFocused(true);
        onFocus?.(event);
      }}
      onBlur={(event) => {
        setFocused(false);
        onBlur?.(event);
      }}
      {...props}
    />
  );
};
