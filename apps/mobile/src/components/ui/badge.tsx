import { Text } from "@/components/ui/text.js";
import { cn } from "@/lib/utils.js";
import { cva, type VariantProps } from "class-variance-authority";
import { View } from "react-native";

const badgeVariants = cva("self-start rounded-full border border-transparent px-2 py-0.5", {
  variants: {
    variant: {
      default: "bg-primary",
      secondary: "bg-secondary",
      destructive: "bg-destructive/20",
      outline: "border-border",
    },
  },
  defaultVariants: { variant: "default" },
});

const labelVariants = cva("text-xs font-medium", {
  variants: {
    variant: {
      default: "text-primary-foreground",
      secondary: "text-secondary-foreground",
      destructive: "text-destructive",
      outline: "text-foreground",
    },
  },
  defaultVariants: { variant: "default" },
});

export const Badge = (
  props: VariantProps<typeof badgeVariants> & {
    readonly children: string;
    readonly className?: string;
  },
) => (
  <View className={cn(badgeVariants({ variant: props.variant }), props.className)}>
    <Text className={labelVariants({ variant: props.variant })}>{props.children}</Text>
  </View>
);
