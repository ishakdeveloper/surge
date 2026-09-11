import { Text } from "@/components/ui/text.js";
import { cn } from "@/lib/utils.js";
import { cva, type VariantProps } from "class-variance-authority";
import * as React from "react";
import { View } from "react-native";

const alertVariants = cva("gap-1 rounded-lg border bg-card px-3 py-2.5", {
  variants: {
    variant: {
      default: "border-border",
      destructive: "border-destructive/40",
    },
  },
  defaultVariants: { variant: "default" },
});

/**
 * A persistent message beside what it is about — this repo's replacement for a
 * toast, per `knowledge/rules/accessible-notifications-and-messages.md`.
 *
 * `accessibilityRole="alert"` so VoiceOver and TalkBack announce it when it
 * appears, and a polite live region for Android, which is how a failed tap is
 * reported where it happened.
 */
export const Alert = (
  { className, variant, ...props }:
    & React.ComponentProps<typeof View>
    & VariantProps<typeof alertVariants>,
) => (
  <View
    accessibilityRole="alert"
    accessibilityLiveRegion="polite"
    className={cn(alertVariants({ variant }), className)}
    {...props}
  />
);

export const AlertTitle = ({ className, ...props }: React.ComponentProps<typeof Text>) => (
  <Text className={cn("font-medium", className)} {...props} />
);

export const AlertDescription = ({ className, ...props }: React.ComponentProps<typeof Text>) => (
  <Text className={cn("text-muted-foreground", className)} {...props} />
);
