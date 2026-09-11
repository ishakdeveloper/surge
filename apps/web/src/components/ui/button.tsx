import { Button as ButtonPrimitive } from "@base-ui/react/button";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";

/**
 * Every button is a pill, and its colour says what it does, as on the rider's
 * sheet: yellow is the next step, black a service, a grey tile a secondary
 * choice, red a way out. It gives a little under the pointer; with reduced
 * motion it only changes colour.
 */
const buttonVariants = cva(
  "group/button inline-flex shrink-0 cursor-pointer items-center justify-center gap-2 rounded-full font-semibold whitespace-nowrap transition-[background-color,color,transform,box-shadow] duration-150 ease-out select-none active:not-aria-[haspopup]:scale-[0.97] disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-45 aria-invalid:shadow-[inset_0_0_0_2px_var(--destructive)] [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground hover:bg-[#f5c900]",
        secondary: "bg-secondary text-secondary-foreground hover:bg-secondary/85",
        outline: "bg-tile text-foreground hover:bg-tile-hover aria-expanded:bg-tile-hover",
        ghost: "text-foreground hover:bg-tile aria-expanded:bg-tile",
        destructive: "bg-destructive/10 text-destructive hover:bg-destructive/15",
        link:
          "rounded-md text-foreground underline decoration-foreground/30 underline-offset-4 hover:decoration-foreground active:scale-100",
      },
      size: {
        default: "h-11 px-5 text-[15px]",
        xs: "h-7 gap-1 px-3 text-xs [&_svg:not([class*='size-'])]:size-3",
        sm: "h-9 gap-1.5 px-4 text-sm [&_svg:not([class*='size-'])]:size-3.5",
        lg: "h-12 px-6 text-[15px]",
        icon: "size-10",
        "icon-xs": "size-7 [&_svg:not([class*='size-'])]:size-3.5",
        "icon-sm": "size-9",
        "icon-lg": "size-12",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  },
);

function Button({
  className,
  variant = "default",
  size = "default",
  ...props
}: ButtonPrimitive.Props & VariantProps<typeof buttonVariants>) {
  return (
    <ButtonPrimitive
      data-slot="button"
      className={cn(buttonVariants({ variant, size, className }))}
      {...props}
    />
  );
}

export { Button, buttonVariants };
