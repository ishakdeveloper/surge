import { Text } from "@/components/ui/text.js";
import { colors } from "@/lib/theme.js";
import { cn } from "@/lib/utils.js";
import Ionicons from "@expo/vector-icons/Ionicons";
import { cva } from "class-variance-authority";
import * as React from "react";
import { Pressable, View } from "react-native";

/**
 * A card on the rider's sheet — the web's `components/sign/sign.tsx` for the
 * phone.
 *
 * Its colour is fixed by what it says, never chosen for emphasis: `direction`
 * (yellow) is the route's next step and the choice made, `choice` (a grey
 * tile) is something to pick or read, `service` (black) is the card or a
 * receipt, `done` (green) is finished, `refused` (red) is not going to happen.
 *
 * React Native does not inherit colour from a view, so a card tells the text
 * and icons inside it what ink to use through context rather than every string
 * restating it.
 */
export type Tone = "direction" | "choice" | "service" | "done" | "refused";

export const signVariants = cva("flex-row items-center gap-3 rounded-2xl px-4 py-3.5", {
  variants: {
    tone: {
      direction: "bg-primary",
      choice: "bg-tile",
      service: "bg-secondary",
      done: "bg-success",
      refused: "bg-destructive",
    },
  },
  defaultVariants: { tone: "choice" },
});

const TEXT: Record<Tone, string> = {
  direction: "text-primary-foreground",
  choice: "text-foreground",
  service: "text-secondary-foreground",
  done: "text-success-foreground",
  refused: "text-destructive-foreground",
};

const INK: Record<Tone, string> = {
  direction: colors.foreground,
  choice: colors.foreground,
  service: "#ffffff",
  done: "#ffffff",
  refused: "#ffffff",
};

const ToneContext = React.createContext<Tone>("choice");

/** The ink of the card this stands on, for what takes a colour rather than a class. */
export const useSignInk = () => INK[React.useContext(ToneContext)];

export const Sign = (
  { tone = "choice", className, ...props }:
    & React.ComponentProps<typeof View>
    & { readonly tone?: Tone; },
) => (
  <ToneContext.Provider value={tone}>
    <View className={cn(signVariants({ tone }), className)} {...props} />
  </ToneContext.Provider>
);

/**
 * A card that is itself the control: a class to pick, a trip to take. It
 * settles a little under the thumb.
 */
export const SignButton = (
  { tone = "choice", className, ...props }:
    & React.ComponentProps<typeof Pressable>
    & { readonly tone?: Tone; },
) => (
  <ToneContext.Provider value={tone}>
    <Pressable
      accessibilityRole="button"
      className={cn(signVariants({ tone }), "active:scale-[0.985] active:opacity-90", className)}
      {...props}
    />
  </ToneContext.Provider>
);

/** Text in the card's own ink. */
export const SignText = ({ className, ...props }: React.ComponentProps<typeof Text>) => {
  const tone = React.useContext(ToneContext);
  return <Text className={cn("text-[15px]", TEXT[tone], className)} {...props} />;
};

/**
 * A fixed-width pictogram column, for cards that carry a bare icon rather than
 * one in a well: the hold step, the account's card.
 */
export const SignGlyph = (props: { readonly children: React.ReactNode; }) => (
  <View
    className="w-7 items-center"
    accessibilityElementsHidden
    importantForAccessibility="no-hide-descendants"
  >
    {props.children}
  </View>
);

/** A bare Ionicon in the card's ink, at the pictogram column's size. */
export const SignIcon = (
  props: { readonly name: React.ComponentProps<typeof Ionicons>["name"]; },
) => <Ionicons name={props.name} size={24} color={useSignInk()} />;

/**
 * An icon in a round well, the way the rider's cards carry theirs: white on a
 * grey tile or a yellow one, a translucent white on black.
 */
export const IconBubble = (props: {
  readonly name: React.ComponentProps<typeof Ionicons>["name"];
  readonly className?: string;
}) => {
  const tone = React.useContext(ToneContext);
  const dark = tone === "service" || tone === "done" || tone === "refused";
  return (
    <View
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
      className={cn(
        "size-10 items-center justify-center rounded-full",
        dark ? "bg-white/15" : "bg-card",
        props.className,
      )}
    >
      <Ionicons name={props.name} size={20} color={INK[tone]} />
    </View>
  );
};

const ACTION_BOX = {
  primary: "bg-primary active:bg-[#f0c400]",
  quiet: "active:bg-tile",
  danger: "active:bg-destructive/10",
} as const;

const ACTION_TEXT = {
  primary: "text-primary-foreground",
  quiet: "text-foreground",
  danger: "text-destructive",
} as const;

/**
 * An action, always a pill: `primary` is the yellow of the next step,
 * `quiet` a secondary choice, `danger` a way out that cannot be taken back.
 */
export const Action = (props: {
  readonly label: string;
  readonly onPress: () => void;
  readonly tone: keyof typeof ACTION_BOX;
  readonly size: "sm" | "block";
  readonly disabled: boolean;
}) => (
  <Pressable
    accessibilityRole="button"
    accessibilityState={{ disabled: props.disabled }}
    disabled={props.disabled}
    onPress={props.onPress}
    hitSlop={props.size === "sm" ? 8 : 0}
    className={cn(
      "items-center justify-center rounded-full active:scale-[0.97]",
      props.size === "sm" ? "h-8 px-3.5" : "h-12 w-full px-6",
      props.disabled && props.tone === "primary" ? "bg-tile" : ACTION_BOX[props.tone],
    )}
  >
    <Text
      className={cn(
        "font-semibold",
        props.size === "sm" ? "text-sm" : "text-[15px]",
        props.disabled ? "text-muted-foreground" : ACTION_TEXT[props.tone],
      )}
    >
      {props.label}
    </Text>
  </Pressable>
);
