import { Heading, Text } from "@/components/ui/text.js";
import { cn } from "@/lib/utils.js";
import * as React from "react";
import { View } from "react-native";

/**
 * A white card on the near-white ground, lifted by a soft shadow rather than
 * outlined — tone and depth, never a border.
 */
export const Card = ({ className, style, ...props }: React.ComponentProps<typeof View>) => (
  <View
    className={cn("gap-3 rounded-3xl bg-card p-5", className)}
    style={[
      {
        shadowColor: "#000000",
        shadowOpacity: 0.06,
        shadowRadius: 14,
        shadowOffset: { width: 0, height: 6 },
        elevation: 2,
      },
      style,
    ]}
    {...props}
  />
);

export const CardTitle = ({ className, ...props }: React.ComponentProps<typeof Heading>) => (
  <Heading className={cn("text-base", className)} {...props} />
);

export const CardDescription = ({ className, ...props }: React.ComponentProps<typeof Text>) => (
  <Text className={cn("text-muted-foreground", className)} {...props} />
);
