import { cn } from "@/lib/utils.js";
import * as React from "react";
import { Text as NativeText } from "react-native";

/**
 * Text, in the app's face, colour and size.
 *
 * React Native inherits neither font nor colour from a parent view the way the
 * web inherits them from `body`, so every string goes through here or it
 * renders in the system face, in black.
 */
export const Text = ({ className, ...props }: React.ComponentProps<typeof NativeText>) => (
  <NativeText className={cn("font-sans text-sm text-foreground", className)} {...props} />
);

/** A section heading, announced as one. */
export const Heading = ({ className, ...props }: React.ComponentProps<typeof NativeText>) => (
  <Text
    accessibilityRole="header"
    className={cn("text-[15px] font-semibold", className)}
    {...props}
  />
);
