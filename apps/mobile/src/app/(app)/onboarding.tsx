import { sessionAtom } from "@/atom/session-atoms.js";
import { type MapMarker, SurgeMap } from "@/components/map/surge-map.js";
import { useCity } from "@/components/map/use-city.js";
import { PressableScale } from "@/components/motion/pressable-scale.js";
import { ProfileEditor } from "@/components/profile/profile-editor.js";
import { IconBubble, Sign, SignText } from "@/components/sign/sign.js";
import { Button } from "@/components/ui/button.js";
import { Text } from "@/components/ui/text.js";
import { PayoutAccount } from "@/drive/earnings.js";
import { rememberOnboarded } from "@/lib/onboarded.js";
import { colors } from "@/lib/theme.js";
import { cn } from "@/lib/utils.js";
import { CardActionError, CardSummary, useCardAction } from "@/ride/card-summary.js";
import { StopName } from "@/ride/place-field.js";
import { RideLayout } from "@/ride/ride-layout.js";
import { useAtomMount, useAtomSet, useAtomValue } from "@effect/atom-react";
import Ionicons from "@expo/vector-icons/Ionicons";
import { insideAmsterdam } from "@surge/client/Places";
import { locateMe } from "@surge/common/atom/driver-atoms";
import { cardAtom, paymentPushesAtom, payoutAccountAtom } from "@surge/common/atom/payment-atoms";
import { myProfileAtom } from "@surge/common/atom/profile-atoms";
import type { LatLng } from "@surge/domain/geo/Polyline";
import { Cause, Option } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";
import { Redirect, router } from "expo-router";
import * as React from "react";
import { View } from "react-native";
import Animated, {
  Easing,
  FadeInDown,
  FadeInLeft,
  FadeInRight,
  ReduceMotion,
  useAnimatedStyle,
  useSharedValue,
  withTiming,
  ZoomIn,
} from "react-native-reanimated";

/**
 * A new account's first minute — the web's onboarding for the phone: what
 * Surge does, the name and face the other side of every trip sees, the one
 * thing it needs before the first trip — a card to hold a fare on, or a
 * payout account to be paid into — and the location that makes the first
 * booking or shift a tap, then straight into it. Every step can be skipped,
 * and none is a tutorial: each is the real thing, done here once instead of
 * discovered later.
 *
 * It stands on the rider's sheet over the map, so the product is the backdrop
 * from the first screen; finding the person moves the map to them.
 */

type Setup = "rider" | "driver";
type Step = "welcome" | "profile" | "card" | "payouts" | "location" | "ready";

const STEPS: Record<Setup, ReadonlyArray<Step>> = {
  rider: ["welcome", "profile", "card", "location", "ready"],
  driver: ["welcome", "profile", "payouts", "location", "ready"],
};

const OnboardingScreen = () => {
  const session = useAtomValue(sessionAtom);
  if (!AsyncResult.isSuccess(session)) return null;
  const { role, userId } = session.value;
  if (role !== "rider" && role !== "driver") return <Redirect href="/account" />;
  return <Onboarding setup={role} userId={userId} />;
};

const Onboarding = (props: { readonly setup: Setup; readonly userId: string; }) => {
  // A card saved through Stripe, or payouts finished there, arrive by push.
  useAtomMount(paymentPushesAtom);
  const steps = STEPS[props.setup];
  const [index, setIndex] = React.useState(0);
  const [direction, setDirection] = React.useState<1 | -1>(1);
  const step = steps[index] ?? "welcome";
  const city = useCity();

  const located = useAtomValue(locateMe);
  const locate = useAtomSet(locateMe);
  const here = AsyncResult.isSuccess(located) ? Option.some(located.value) : Option.none();
  const cardSaved = useAtomValue(
    cardAtom,
    (card) => AsyncResult.isSuccess(card) && card.value.saved,
  );
  const payoutsReady = useAtomValue(
    payoutAccountAtom,
    (account) =>
      AsyncResult.isSuccess(account)
      && account.value.status !== "PAYOUT_ACCOUNT_STATUS_NOT_STARTED"
      && !account.value.requirementsDue,
  );
  const named = useAtomValue(
    myProfileAtom,
    (profile) => AsyncResult.isSuccess(profile) && profile.value.displayName.trim() !== "",
  );

  const move = (to: number) => {
    setDirection(to > index ? 1 : -1);
    setIndex(Math.max(0, Math.min(steps.length - 1, to)));
  };
  const next = () => {
    move(index + 1);
  };
  const finish = () => {
    rememberOnboarded(props.userId);
    router.replace(props.setup === "rider" ? "/ride" : "/drive");
  };

  const markers: ReadonlyArray<MapMarker> = Option.match(here, {
    onNone: () => [],
    onSome: (position) => [{ id: "here", position, kind: "self" }],
  });

  // The step's own action, where it has one, lives in its content; the footer
  // always offers the way on — "Continue" once the step is done, a quieter
  // "Skip for now" while it is not.
  const stepDone = step === "profile"
    ? named
    : step === "card"
    ? cardSaved
    : step === "payouts"
    ? payoutsReady
    : step === "location"
    ? Option.isSome(here)
    : true;

  const footer = step === "ready"
    ? (
      <Button className="h-14" feedback="success" onPress={finish}>
        {props.setup === "rider" ? "Book your first ride" : "Start your first shift"}
      </Button>
    )
    : step === "location" && !stepDone
    ? (
      <View className="gap-1">
        <Button
          className="h-14"
          disabled={located.waiting}
          onPress={() => {
            locate();
          }}
        >
          {located.waiting
            ? "Finding you…"
            : AsyncResult.isFailure(located)
            ? "Try again"
            : "Use my location"}
        </Button>
        <Button variant="ghost" onPress={next}>Skip for now</Button>
      </View>
    )
    : step === "welcome"
    ? (
      <View className="gap-1">
        <Button className="h-14" onPress={next}>Get started</Button>
        <Button variant="ghost" onPress={finish}>Skip setup</Button>
      </View>
    )
    : stepDone
    ? <Button className="h-14" onPress={next}>Continue</Button>
    : <Button variant="ghost" onPress={next}>Skip for now</Button>;

  const enter = (direction === 1 ? FadeInRight : FadeInLeft).duration(240).reduceMotion(
    ReduceMotion.System,
  );

  return (
    <RideLayout
      footer={footer}
      map={
        <SurgeMap
          markers={markers}
          route={[]}
          follow={Option.toArray(here)}
          cars={city.cars}
          flashes={city.flashes}
          onView={city.onView}
        />
      }
    >
      <View className="gap-5 px-1 pt-1 pb-2">
        <View className="h-8 flex-row items-center gap-3">
          {index > 0 && step !== "ready" && (
            <PressableScale
              accessibilityLabel="Back"
              hitSlop={8}
              scaleTo={0.9}
              onPress={() => {
                move(index - 1);
              }}
              className="size-8 items-center justify-center rounded-full bg-tile active:bg-tile-hover"
            >
              <Ionicons name="arrow-back" size={16} color={colors.foreground} />
            </PressableScale>
          )}
          <Progress total={steps.length} index={index} />
        </View>

        <Animated.View key={step} entering={enter} className="gap-5">
          {step === "welcome" && <Welcome setup={props.setup} />}
          {step === "profile" && <ProfileStep setup={props.setup} />}
          {step === "card" && <CardStep saved={cardSaved} />}
          {step === "payouts" && <PayoutsStep />}
          {step === "location" && <LocationStep setup={props.setup} located={located} />}
          {step === "ready" && (
            <Ready
              setup={props.setup}
              done={[
                { label: "Your name and photo", done: named },
                props.setup === "rider"
                  ? { label: "Card for your fares", done: cardSaved }
                  : { label: "Payouts with Stripe", done: payoutsReady },
                { label: "Your location", done: Option.isSome(here) },
              ]}
            />
          )}
        </Animated.View>
      </View>
    </RideLayout>
  );
};

/** How far along: one segment per step, filling as each is reached. */
const Progress = (props: { readonly total: number; readonly index: number; }) => (
  <View className="flex-1 flex-row items-center gap-3">
    <View
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
      className="flex-1 flex-row gap-1.5"
    >
      {Array.from(
        { length: props.total },
        (_, segment) => <Segment key={segment} filled={segment <= props.index} />,
      )}
    </View>
    <Text className="text-xs font-semibold text-muted-foreground tabular-nums">
      Step {props.index + 1} of {props.total}
    </Text>
  </View>
);

const Segment = (props: { readonly filled: boolean; }) => {
  const fill = useSharedValue(props.filled ? 1 : 0);
  React.useEffect(() => {
    fill.value = withTiming(props.filled ? 1 : 0, {
      duration: 400,
      easing: Easing.out(Easing.exp),
      reduceMotion: ReduceMotion.System,
    });
  }, [props.filled, fill]);
  const width = useAnimatedStyle(() => ({ width: `${fill.value * 100}%` }));
  return (
    <View className="h-1.5 flex-1 overflow-hidden rounded-full bg-tile">
      <Animated.View className="h-full rounded-full bg-foreground" style={width} />
    </View>
  );
};

const StepHeader = (props: { readonly title: string; readonly text: string; }) => (
  <View className="gap-1.5">
    <Text accessibilityRole="header" className="text-2xl leading-tight font-semibold">
      {props.title}
    </Text>
    <Text className="text-[15px] leading-5 text-muted-foreground">{props.text}</Text>
  </View>
);

/** A green check that pops in when a step is done. */
const DoneMark = () => (
  <Animated.View
    entering={ZoomIn.springify().damping(14).reduceMotion(ReduceMotion.System)}
    accessibilityElementsHidden
    importantForAccessibility="no-hide-descendants"
    className="size-7 items-center justify-center rounded-full bg-success"
  >
    <Ionicons name="checkmark" size={17} color="#ffffff" />
  </Animated.View>
);

/**
 * How it works, in three stops on a rail like the rider's From and To — a
 * sequence, so it is numbered, and each stop rises in after the one before.
 */
const HowItWorks = (
  props: { readonly stops: ReadonlyArray<{ readonly title: string; readonly text: string; }>; },
) => (
  <View className="gap-4 rounded-2xl bg-tile p-4">
    <View className="absolute top-8 bottom-8 left-[27px] w-0.5 rounded-full bg-foreground/15" />
    {props.stops.map((stop, index) => (
      <Animated.View
        key={stop.title}
        entering={FadeInDown.delay(80 + index * 70).duration(260).reduceMotion(ReduceMotion.System)}
        className="flex-row items-start gap-3"
      >
        <View className="size-6 items-center justify-center rounded-full bg-card">
          <Text className="text-xs font-semibold tabular-nums">{index + 1}</Text>
        </View>
        <View className="min-w-0 flex-1">
          <Text className="text-[15px] font-semibold">{stop.title}</Text>
          <Text className="text-[13px] text-muted-foreground">{stop.text}</Text>
        </View>
      </Animated.View>
    ))}
  </View>
);

const Welcome = (props: { readonly setup: Setup; }) =>
  props.setup === "rider"
    ? (
      <>
        <StepHeader
          title="Welcome to Surge"
          text="Rides across Amsterdam, with the price in front of you before you book."
        />
        <HowItWorks
          stops={[
            { title: "Say where to", text: "Your pickup starts where you are." },
            {
              title: "See the price first",
              text: "It is held on your card, and charged when the trip ends.",
            },
            { title: "Ride", text: "Follow your driver on the map, and message them." },
          ]}
        />
      </>
    )
    : (
      <>
        <StepHeader
          title="Welcome to Surge"
          text="Drive in Amsterdam when it suits you: go on shift, and trips come to you."
        />
        <HowItWorks
          stops={[
            { title: "Go on shift", text: "Dispatch offers you trips near where you are." },
            { title: "Take the trips you want", text: "Accept or pass on each offer." },
            {
              title: "Get paid",
              text: "Your share of each fare goes to your balance, paid out by Stripe.",
            },
          ]}
        />
      </>
    );

/** The first name and the photo the other side of every trip sees. */
const ProfileStep = (props: { readonly setup: Setup; }) => (
  <>
    <StepHeader
      title="Your name and photo"
      text={props.setup === "rider"
        ? "Your driver sees them, so they know who they are picking up."
        : "Riders look for your face and your first name at the kerb. A clear photo of you, not the car."}
    />
    <ProfileEditor role={props.setup} explain={false} />
  </>
);

const CardStep = (props: { readonly saved: boolean; }) => {
  const action = useCardAction();
  return (
    <>
      <StepHeader
        title="Add your card"
        text="Your fare is held on it when you book, and charged only when the trip ends. A cancelled trip releases the hold."
      />
      <Sign tone="service">
        <IconBubble name="card-outline" />
        <View className="min-w-0 flex-1">
          <CardSummary />
        </View>
        {props.saved && <DoneMark />}
      </Sign>
      {!props.saved && (
        <Button disabled={action.waiting} onPress={action.run}>
          {action.label}
        </Button>
      )}
      <CardActionError />
    </>
  );
};

const PayoutsStep = () => (
  <>
    <StepHeader
      title="Get paid"
      text="Surge pays your share of each fare through Stripe. Set it up once; what you earn before then is kept, and paid once you finish."
    />
    <PayoutAccount />
  </>
);

/** Why the position did not come, in words a person can act on. */
const whyNoPosition = (cause: Cause.Cause<unknown>): string => {
  const error = Option.getOrUndefined(Cause.findErrorOption(cause));
  const reason = typeof error === "object" && error !== null && "reason" in error
    ? error.reason
    : undefined;
  return reason === "Denied"
    ? "Location is switched off for Surge. Turn it on in Settings, or type your pickup instead."
    : reason === "Unsupported"
    ? "This phone cannot share a location. You can type your pickup instead."
    : "We could not find you just now. Try again, or type your pickup instead.";
};

const LocationStep = (props: {
  readonly setup: Setup;
  readonly located: AsyncResult.AsyncResult<LatLng, unknown>;
}) => {
  const { located } = props;
  return (
    <>
      <StepHeader
        title={props.setup === "rider" ? "Start where you are" : "Share your position on shift"}
        text={props.setup === "rider"
          ? "Surge sets your pickup to where you are, so you only type where you are going."
          : "Dispatch offers you trips near you, so it needs your position while you are on shift, and never off it."}
      />
      {AsyncResult.isSuccess(located)
        ? insideAmsterdam(located.value)
          ? (
            <Sign tone="done">
              <IconBubble name="locate" />
              <SignText className="min-w-0 flex-1 font-semibold">
                You are near <StopName stop={{ position: located.value, place: Option.none() }} />
              </SignText>
              <DoneMark />
            </Sign>
          )
          : (
            <Sign>
              <IconBubble name="locate-outline" />
              <SignText className="min-w-0 flex-1">
                You are outside Amsterdam, the one city Surge runs in, so you will type your pickup
                there.
              </SignText>
            </Sign>
          )
        : AsyncResult.isFailure(located) && !located.waiting && (
          <Sign>
            <SignText>{whyNoPosition(located.cause)}</SignText>
          </Sign>
        )}
    </>
  );
};

const Ready = (props: {
  readonly setup: Setup;
  readonly done: ReadonlyArray<{ readonly label: string; readonly done: boolean; }>;
}) => (
  <>
    <Animated.View
      entering={ZoomIn.springify().damping(12).stiffness(260).reduceMotion(ReduceMotion.System)}
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
      className="size-14 items-center justify-center rounded-full bg-success"
    >
      <Ionicons name="checkmark" size={30} color="#ffffff" />
    </Animated.View>
    <StepHeader
      title="You are set"
      text={props.setup === "rider"
        ? "Choose where you are going, and see the price before you book."
        : "Go online from the Drive tab, and offers start coming in."}
    />
    <View className="gap-2">
      {props.done.map((item, index) => (
        <Animated.View
          key={item.label}
          entering={FadeInDown.delay(120 + index * 60).duration(240).reduceMotion(
            ReduceMotion.System,
          )}
          className="flex-row items-center gap-3 rounded-2xl bg-tile px-4 py-3"
        >
          <View
            className={cn(
              "size-6 items-center justify-center rounded-full",
              item.done ? "bg-success" : "border-2 border-foreground/20",
            )}
          >
            {item.done && <Ionicons name="checkmark" size={14} color="#ffffff" />}
          </View>
          <Text className="flex-1 text-[15px] font-semibold">{item.label}</Text>
          <Text className="text-[13px] text-muted-foreground">
            {item.done ? "Done" : "Later"}
          </Text>
        </Animated.View>
      ))}
    </View>
  </>
);

export default OnboardingScreen;
