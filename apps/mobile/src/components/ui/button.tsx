import { Text } from "@/components/ui/text.js";
import { cn } from "@/lib/utils.js";
import { cva, type VariantProps } from "class-variance-authority";
import * as React from "react";
import { Pressable } from "react-native";

/**
 * The web app's button variants, sized for a thumb: 44pt tall by default,
 * which is Apple's minimum touch target and Android's 48dp near enough.
 */
const buttonVariants = cva(
  "flex-row items-center justify-center rounded-lg border border-transparent px-4 active:opacity-80",
  {
    variants: {
      variant: {
        default: "bg-primary",
        outline: "border-border bg-input/30",
        secondary: "bg-secondary",
        ghost: "",
        destructive: "bg-destructive/20",
      },
      size: {
        default: "h-11",
        sm: "h-9 px-3",
      },
    },
    defaultVariants: { variant: "default", size: "default" },
  },
);

const labelVariants = cva("font-medium", {
  variants: {
    variant: {
      default: "text-primary-foreground",
      outline: "text-foreground",
      secondary: "text-secondary-foreground",
      ghost: "text-foreground",
      destructive: "text-destructive",
    },
    size: {
      default: "text-sm",
      sm: "text-xs",
    },
  },
  defaultVariants: { variant: "default", size: "default" },
});

export const Button = ({
  className,
  variant,
  size,
  disabled,
  children,
  ...props
}:
  & Omit<React.ComponentProps<typeof Pressable>, "children">
  & VariantProps<typeof buttonVariants>
  & { readonly children: string; }) => (
  <Pressable
    accessibilityRole="button"
    accessibilityState={{ disabled: disabled === true }}
    disabled={disabled}
    className={cn(buttonVariants({ variant, size }), disabled === true && "opacity-50", className)}
    {...props}
  >
    <Text className={labelVariants({ variant, size })}>{children}</Text>
  </Pressable>
);
