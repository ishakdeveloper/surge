import { Text } from "@/components/ui/text.js";
import { cn } from "@/lib/utils.js";
import * as React from "react";
import { View } from "react-native";

/**
 * The web's `<dl>` grids — a label column and a value column — which React
 * Native has no element for.
 *
 * `mono` for identifiers and raw enums, which `dashboard-ui.md` keeps exact and
 * the web sets in `font-mono text-xs`.
 */
export const Details = (props: { readonly children: React.ReactNode; }) => (
  <View className="gap-1.5">{props.children}</View>
);

export const Detail = (props: {
  readonly label: string;
  readonly value: string;
  readonly mono?: boolean;
}) => (
  <View className="flex-row gap-3">
    <Text className="w-20 text-muted-foreground">{props.label}</Text>
    <Text
      selectable
      className={cn("flex-1", props.mono === true && "font-mono text-xs leading-5")}
    >
      {props.value}
    </Text>
  </View>
);
