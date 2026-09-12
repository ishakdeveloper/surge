import { PressableScale } from "@/components/motion/pressable-scale.js";
import { Text } from "@/components/ui/text.js";
import { cn } from "@/lib/utils.js";
import { cva, type VariantProps } from "class-variance-authority";
import type * as React from "react";

/**
 * The web app's buttons, as the rider's world draws them: pills, no borders,
 * 48pt tall so a thumb never misses. Yellow is the next step, ink a service,
 * the grey tile a second choice, and a destructive one says so in red. They
 * settle under the thumb and tap back.
 */
const buttonVariants = cva("flex-row items-center justify-center rounded-full px-5", {
  variants: {
    variant: {
      default: "bg-primary active:bg-[#f0c400]",
      outline: "bg-tile active:bg-tile-hover",
      secondary: "bg-secondary active:opacity-90",
      ghost: "active:bg-tile",
      destructive: "bg-destructive/10 active:bg-destructive/15",
    },
    size: {
      default: "h-12",
      sm: "h-9 px-4",
    },
  },
  defaultVariants: { variant: "default", size: "default" },
});

const labelVariants = cva("font-semibold", {
  variants: {
    variant: {
      default: "text-primary-foreground",
      outline: "text-foreground",
      secondary: "text-secondary-foreground",
      ghost: "text-foreground",
      destructive: "text-destructive",
    },
    size: {
      default: "text-[15px]",
      sm: "text-[13px]",
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
  & Omit<React.ComponentProps<typeof PressableScale>, "children">
  & VariantProps<typeof buttonVariants>
  & { readonly children: string; }) => (
  <PressableScale
    accessibilityState={{ disabled: disabled === true }}
    disabled={disabled}
    className={cn(buttonVariants({ variant, size }), disabled === true && "opacity-50", className)}
    {...props}
  >
    <Text className={labelVariants({ variant, size })}>{children}</Text>
  </PressableScale>
);
