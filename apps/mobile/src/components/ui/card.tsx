import { Heading, Text } from "@/components/ui/text.js";
import { cn } from "@/lib/utils.js";
import * as React from "react";
import { View } from "react-native";

/** The web's bordered panel — `rounded-md border border-border p-4` — as one component. */
export const Card = ({ className, ...props }: React.ComponentProps<typeof View>) => (
  <View className={cn("gap-3 rounded-xl border border-border bg-card p-4", className)} {...props} />
);

export const CardTitle = ({ className, ...props }: React.ComponentProps<typeof Heading>) => (
  <Heading className={cn("text-base", className)} {...props} />
);

export const CardDescription = ({ className, ...props }: React.ComponentProps<typeof Text>) => (
  <Text className={cn("text-muted-foreground", className)} {...props} />
);
