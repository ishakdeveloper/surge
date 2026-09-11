import { cn } from "@/lib/utils.js";
import { cva, type VariantProps } from "class-variance-authority";
import type * as React from "react";

/**
 * A card on the rider's sheet: the one container these screens are built from.
 *
 * Its colour is fixed by what it says, never chosen for emphasis. `direction`
 * (yellow) is the route's next step and the choice made, `choice` (a grey
 * tile) is something to pick or read, `service` (black) is the card, the
 * account or a receipt, `done` (green) is finished, `refused` (red) is not
 * going to happen. The names are the wayfinding sign's, which is where the
 * colour rule comes from.
 */
export const signVariants = cva("flex gap-3 rounded-2xl px-4 py-3.5", {
  variants: {
    tone: {
      direction: "bg-primary text-primary-foreground",
      choice: "bg-tile text-foreground",
      service: "bg-secondary text-secondary-foreground",
      done: "bg-success text-success-foreground",
      refused: "bg-destructive text-destructive-foreground",
    },
  },
  defaultVariants: { tone: "choice" },
});

type Tone = NonNullable<VariantProps<typeof signVariants>["tone"]>;

/**
 * The tones a black-on-light focus ring would vanish on. They mark themselves
 * with `data-surface="dark"`, and `app.css` turns the ring yellow inside them.
 */
const DARK: ReadonlySet<Tone> = new Set(["service", "done", "refused"]);

/**
 * The give of a card that is itself a control: it settles a little under the
 * pointer and lands on its new colour. Reduced motion keeps only the colour.
 */
export const pressable =
  "cursor-pointer transition-[background-color,color,transform] duration-150 ease-out active:scale-[0.985] disabled:cursor-not-allowed disabled:active:scale-100";

/**
 * An action, always a pill. `primary` is the yellow of the next step, `quiet`
 * a secondary choice, `danger` a way out that cannot be taken back.
 */
export const actionVariants = cva(
  "inline-flex shrink-0 cursor-pointer items-center justify-center gap-2 rounded-full font-semibold whitespace-nowrap transition-[background-color,color,transform] duration-150 ease-out active:scale-[0.97] disabled:cursor-not-allowed disabled:active:scale-100",
  {
    variants: {
      tone: {
        primary:
          "bg-primary text-primary-foreground hover:bg-[#f5c900] disabled:bg-tile disabled:text-muted-foreground",
        quiet: "text-foreground hover:bg-tile disabled:text-muted-foreground",
        danger: "text-destructive hover:bg-destructive/10 disabled:text-muted-foreground",
      },
      size: {
        sm: "h-8 px-3.5 text-sm",
        md: "h-11 px-5 text-[15px]",
        block: "h-12 w-full px-6 text-[15px]",
      },
    },
    defaultVariants: { tone: "primary", size: "md" },
  },
);

export const Sign = (
  { tone, className, ...props }: React.ComponentProps<"div"> & VariantProps<typeof signVariants>,
) => (
  <div
    data-surface={tone !== null && tone !== undefined && DARK.has(tone) ? "dark" : undefined}
    className={cn(signVariants({ tone }), className)}
    {...props}
  />
);

/** A fixed-width pictogram column, for signs that carry a bare icon. */
export const SignGlyph = ({ className, ...props }: React.ComponentProps<"span">) => (
  <span
    aria-hidden
    className={cn("flex w-7 shrink-0 justify-center [&_svg]:size-6", className)}
    {...props}
  />
);

/**
 * An icon in a round well, the way the rider's cards carry theirs: white on a
 * grey tile or a yellow one, a translucent white on black.
 */
export const IconBubble = ({ className, ...props }: React.ComponentProps<"span">) => (
  <span
    aria-hidden
    className={cn(
      "relative grid size-10 shrink-0 place-items-center rounded-full bg-card [&_svg]:size-5",
      className,
    )}
    {...props}
  />
);

/** The cards of one sheet, a small gap apart. */
export const SignStack = ({ className, ...props }: React.ComponentProps<"div">) => (
  <div className={cn("flex flex-col gap-2", className)} {...props} />
);
