import { colors } from "@/lib/theme.js";
import { cn } from "@/lib/utils.js";
import * as React from "react";
import { TextInput } from "react-native";

export const Input = (
  { className, invalid, ...props }: React.ComponentProps<typeof TextInput> & {
    /** The web's `aria-invalid` styling: a destructive border. */
    readonly invalid?: boolean;
  },
) => (
  <TextInput
    placeholderTextColor={colors.mutedForeground}
    className={cn(
      "h-11 rounded-lg border border-input bg-input/30 px-3 text-base text-foreground",
      invalid === true && "border-destructive",
      className,
    )}
    {...props}
  />
);
