import { cn } from "@/lib/utils.js";
import * as React from "react";
import { Text as NativeText } from "react-native";

/**
 * Text, in the app's colour and size.
 *
 * React Native does not inherit colour from a parent view the way the web
 * inherits it from `body`, so every string goes through here or it renders
 * black on a near-black background.
 */
export const Text = ({ className, ...props }: React.ComponentProps<typeof NativeText>) => (
  <NativeText className={cn("text-sm text-foreground", className)} {...props} />
);

/** A section heading, announced as one. The web's `h2 text-sm font-medium`. */
export const Heading = ({ className, ...props }: React.ComponentProps<typeof NativeText>) => (
  <Text accessibilityRole="header" className={cn("font-medium", className)} {...props} />
);
