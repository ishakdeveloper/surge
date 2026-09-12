import { openOnboarding } from "@/atom/payment-atoms.js";
import { ActionError } from "@/components/app/action-error.js";
import { QueryError } from "@/components/app/query-error.js";
import { type MapMarker, SurgeMap } from "@/components/map/surge-map.js";
import { useCity } from "@/components/map/use-city.js";
import { CardSetup } from "@/components/payments/card-setup.js";
import { ProfileEditor } from "@/components/profile/profile-editor.js";
import { actionVariants, IconBubble, Sign } from "@/components/sign/sign.js";
import { cn } from "@/lib/utils.js";
import { CardSummary } from "@/routes/_protected/ride/-components/card-summary.js";
import { StopName } from "@/routes/_protected/ride/-components/place-field.js";
import { RideLayout, useRideMapInset } from "@/routes/_protected/ride/-components/ride-layout.js";
import { useAtomMount, useAtomSet, useAtomValue } from "@effect/atom-react";
import { insideAmsterdam } from "@surge/client/Places";
import { locateMe } from "@surge/common/atom/driver-atoms";
import { cardAtom, paymentPushesAtom, payoutAccountAtom } from "@surge/common/atom/payment-atoms";
import { myProfileAtom } from "@surge/common/atom/profile-atoms";
import { payoutStatus } from "@surge/common/lib/format";
import type { LatLng } from "@surge/domain/geo/Polyline";
import { Link } from "@tanstack/react-router";
import { Cause, Option } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";
import {
  ArrowLeft,
  ArrowRight,
  Check,
  CreditCard,
  LoaderCircle,
  LocateFixed,
  Wallet,
} from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import * as React from "react";

/**
 * A new account's first minute: what Surge does, the one thing it needs before
 * the first trip — a card to hold a fare on, or a payout account to be paid
 * into — and the location that makes the first booking or shift a tap, then
 * straight into it. Every step can be skipped, and none is a tutorial: each is
 * the real thing, done here once instead of discovered later.
 *
 * It stands on the rider's sheet over the live map, so the product is the
 * backdrop from the first screen; finding the rider moves the map to them.
 */

type Setup = "rider" | "driver";
type Step = "welcome" | "profile" | "card" | "payouts" | "location" | "ready";

// The name and the photo come first after the welcome: they are what the
// other side of every trip sees, and the one step with nothing to set up
// elsewhere.
const STEPS: Record<Setup, ReadonlyArray<Step>> = {
  rider: ["welcome", "profile", "card", "location", "ready"],
  driver: ["welcome", "profile", "payouts", "location", "ready"],
};

const EASE_OUT = [0.22, 1, 0.36, 1] as const;

/**
 * Whether this account has finished or skipped onboarding, in this browser.
 * A convenience, not state anyone else needs: an unreadable store just means
 * the flow shows again, which it can always be skipped from.
 */
const doneKey = (userId: string) => `surge.onboarded.${userId}`;

export const hasOnboarded = (userId: string): boolean => {
  try {
    return globalThis.localStorage.getItem(doneKey(userId)) === "1";
  } catch {
    return false;
  }
};

const rememberOnboarded = (userId: string) => {
  try {
    globalThis.localStorage.setItem(doneKey(userId), "1");
  } catch {
    // Private windows and blocked storage: it shows again next time.
  }
};

export const Onboarding = (props: {
  readonly setup: Setup;
  readonly userId: string;
  /** Skipping from the first step: straight to the product. */
  readonly onSkip: () => void;
}) => {
  // A card saved through Stripe, or payouts finished there, arrive by push.
  useAtomMount(paymentPushesAtom);
  const steps = STEPS[props.setup];
  const [index, setIndex] = React.useState(0);
  const [direction, setDirection] = React.useState<1 | -1>(1);
  const step = steps[index] ?? "welcome";
  const inset = useRideMapInset();
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

  const surface = props.setup === "rider" ? "/ride" : "/drive";
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
      <Link
        to={surface}
        onClick={() => {
          rememberOnboarded(props.userId);
        }}
        className={cn(actionVariants({ size: "block" }), "group h-14 text-[17px] font-bold")}
      >
        {props.setup === "rider" ? "Book your first ride" : "Start your first shift"}
        <ArrowRight className="size-5 transition-transform duration-200 group-hover:translate-x-0.5" />
      </Link>
    )
    : step === "location" && !stepDone
    ? (
      // The step's one action is the next step, so it is pinned under the
      // thumb like every other yellow one, with the way past it beneath.
      <div className="flex flex-col gap-1">
        <button
          type="button"
          disabled={located.waiting}
          onClick={() => {
            locate();
          }}
          className={cn(actionVariants({ size: "block" }), "h-14 text-[17px] font-bold")}
        >
          {located.waiting
            ? <LoaderCircle className="size-5 motion-safe:animate-spin" />
            : <LocateFixed className="size-5" />}
          {located.waiting
            ? "Finding you…"
            : AsyncResult.isFailure(located)
            ? "Try again"
            : "Use my location"}
        </button>
        <button
          type="button"
          onClick={next}
          className={actionVariants({ tone: "quiet", size: "block" })}
        >
          Skip for now
        </button>
      </div>
    )
    : step === "welcome"
    ? (
      <div className="flex flex-col gap-1">
        <button
          type="button"
          onClick={next}
          className={cn(actionVariants({ size: "block" }), "h-14 text-[17px] font-bold")}
        >
          Get started
        </button>
        <button
          type="button"
          onClick={() => {
            rememberOnboarded(props.userId);
            props.onSkip();
          }}
          className={actionVariants({ tone: "quiet", size: "block" })}
        >
          Skip setup
        </button>
      </div>
    )
    : (
      <button
        type="button"
        onClick={next}
        className={cn(
          actionVariants({ tone: stepDone ? "primary" : "quiet", size: "block" }),
          stepDone && "h-14 text-[17px] font-bold",
        )}
      >
        {stepDone ? "Continue" : "Skip for now"}
      </button>
    );

  return (
    <RideLayout
      title={props.setup === "rider" ? "Set up Surge to ride" : "Set up Surge to drive"}
      footer={footer}
      map={
        <SurgeMap
          markers={markers}
          route={[]}
          follow={Option.toArray(here)}
          inset={inset}
          cars={city.cars}
          flashes={city.flashes}
          onView={city.onView}
        />
      }
    >
      <div className="flex flex-col gap-5 px-1 pt-1 pb-2">
        <div className="flex items-center gap-3">
          {index > 0 && step !== "ready" && (
            <button
              type="button"
              aria-label="Back"
              onClick={() => {
                move(index - 1);
              }}
              className="grid size-8 shrink-0 cursor-pointer place-items-center rounded-full bg-tile transition-colors hover:bg-tile-hover"
            >
              <ArrowLeft className="size-4" aria-hidden />
            </button>
          )}
          <Progress total={steps.length} index={index} />
        </div>

        <AnimatePresence mode="wait" initial={false}>
          <motion.div
            key={step}
            initial={{ opacity: 0, x: 20 * direction }}
            animate={{ opacity: 1, x: 0 }}
            exit={{ opacity: 0, x: -20 * direction }}
            transition={{ duration: 0.22, ease: EASE_OUT }}
            className="flex flex-col gap-5"
          >
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
          </motion.div>
        </AnimatePresence>
      </div>
    </RideLayout>
  );
};

/** How far along: one segment per step, filling as each is reached. */
const Progress = (props: { readonly total: number; readonly index: number; }) => (
  <div className="flex flex-1 items-center gap-3">
    <div aria-hidden className="flex flex-1 gap-1.5">
      {Array.from(
        { length: props.total },
        (_, segment) => (
          <span key={segment} className="h-1.5 flex-1 overflow-hidden rounded-full bg-tile">
            <motion.span
              className="block h-full rounded-full bg-foreground"
              initial={false}
              animate={{ width: segment <= props.index ? "100%" : "0%" }}
              transition={{ duration: 0.4, ease: EASE_OUT }}
            />
          </span>
        ),
      )}
    </div>
    <span className="text-xs font-semibold text-muted-foreground tabular-nums">
      Step {props.index + 1} of {props.total}
    </span>
  </div>
);

const StepHeader = (props: { readonly title: string; readonly text: string; }) => (
  <header className="flex flex-col gap-1.5">
    <h2 className="text-2xl leading-tight font-bold text-balance">{props.title}</h2>
    <p className="text-[15px] text-pretty text-muted-foreground">{props.text}</p>
  </header>
);

/** A green check that pops in when a step is done. */
const DoneMark = () => (
  <motion.span
    aria-hidden
    initial={{ scale: 0.4, opacity: 0 }}
    animate={{ scale: 1, opacity: 1 }}
    transition={{ type: "spring", stiffness: 520, damping: 26 }}
    className="grid size-7 shrink-0 place-items-center rounded-full bg-success text-success-foreground"
  >
    <Check className="size-4" strokeWidth={3} />
  </motion.span>
);

/**
 * How it works, in three stops on the same dotted rail as the rider's From
 * and To — a sequence, so it is numbered.
 */
const HowItWorks = (
  props: { readonly stops: ReadonlyArray<{ readonly title: string; readonly text: string; }>; },
) => (
  <ol className="relative flex flex-col gap-4 rounded-2xl bg-tile p-4">
    <span
      aria-hidden
      className="absolute top-7 bottom-7 left-[27px] border-l-2 border-dotted border-foreground/25"
    />
    {props.stops.map((stop, index) => (
      <motion.li
        key={stop.title}
        initial={{ opacity: 0, y: 6 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ delay: 0.08 + index * 0.07, duration: 0.26, ease: EASE_OUT }}
        className="relative flex items-start gap-3"
      >
        <span className="grid size-6 shrink-0 place-items-center rounded-full bg-card text-xs font-bold tabular-nums shadow-[0_0_0_4px_var(--tile)]">
          {index + 1}
        </span>
        <span className="flex flex-col">
          <span className="text-[15px] font-semibold">{stop.title}</span>
          <span className="text-[13px] text-muted-foreground">{stop.text}</span>
        </span>
      </motion.li>
    ))}
  </ol>
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
            { title: "Ride", text: "Follow your driver on the map until they arrive." },
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

const CardStep = (props: { readonly saved: boolean; }) => (
  <>
    <StepHeader
      title="Add your card"
      text="Your fare is held on it when you book, and charged only when the trip ends. A cancelled trip releases the hold."
    />
    <Sign tone="service" className="items-center">
      <IconBubble className="bg-white/10">
        <CreditCard />
      </IconBubble>
      <div className="min-w-0 flex-1">
        <CardSummary />
      </div>
      {props.saved && <DoneMark />}
    </Sign>
    {
      /* On the sheet itself, not in a tile: a sentence and its button. The
        button stays here rather than pinned below because Stripe's form, when
        there is one, owns its own submit. */
    }
    {!props.saved && (
      <div className="flex flex-col gap-3">
        <CardSetup />
      </div>
    )}
  </>
);

const PayoutsStep = () => {
  const account = useAtomValue(payoutAccountAtom);
  const opening = useAtomValue(openOnboarding);
  const open = useAtomSet(openOnboarding);

  const body = (() => {
    if (AsyncResult.isInitial(account)) {
      return (
        <Sign tone="choice" className="items-center" aria-busy>
          <IconBubble>
            <LoaderCircle className="motion-safe:animate-spin" />
          </IconBubble>
          <p className="text-[15px] font-semibold">Checking your payout account…</p>
        </Sign>
      );
    }
    if (AsyncResult.isFailure(account)) {
      return <QueryError result={account} subject="your payout account" />;
    }

    const { status, requirementsDue } = account.value;
    const notStarted = status === "PAYOUT_ACCOUNT_STATUS_NOT_STARTED";
    const ready = !notStarted && !requirementsDue;
    return (
      <>
        <Sign tone="choice" className="items-center">
          <IconBubble>
            <Wallet />
          </IconBubble>
          <div className="flex min-w-0 flex-1 flex-col">
            <span className="text-base font-semibold">Payouts</span>
            <span className="text-[13px] text-muted-foreground">
              {requirementsDue
                ? "Stripe needs more from you"
                : status === "PAYOUT_ACCOUNT_STATUS_PENDING"
                ? "Stripe is checking your details"
                : payoutStatus[status]}
            </span>
          </div>
          {ready && <DoneMark />}
        </Sign>
        {(notStarted || requirementsDue) && (
          <button
            type="button"
            disabled={opening.waiting}
            onClick={() => {
              open();
            }}
            className={actionVariants({ size: "block" })}
          >
            {opening.waiting && <LoaderCircle className="size-4 motion-safe:animate-spin" />}
            {opening.waiting
              ? "Opening Stripe…"
              : notStarted
              ? "Set up payouts with Stripe"
              : "Continue with Stripe"}
          </button>
        )}
        {AsyncResult.isFailure(opening) && <ActionError cause={opening.cause} />}
      </>
    );
  })();

  return (
    <>
      <StepHeader
        title="Get paid"
        text="Surge pays your share of each fare through Stripe. Set it up once; what you earn before then is kept, and paid once you finish."
      />
      {body}
    </>
  );
};

/** Why the position did not come, in words a rider can act on. */
const whyNoPosition = (cause: Cause.Cause<unknown>): string => {
  const error = Option.getOrUndefined(Cause.findErrorOption(cause));
  const reason = typeof error === "object" && error !== null && "reason" in error
    ? error.reason
    : undefined;
  return reason === "Denied"
    ? "Location is switched off for Surge. Turn it on in your browser's settings for this site, or type your pickup instead."
    : reason === "Unsupported"
    ? "This browser cannot share a location. You can type your pickup instead."
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
            <Sign tone="done" className="items-center">
              <IconBubble className="bg-white/15">
                <LocateFixed />
              </IconBubble>
              <p className="min-w-0 flex-1 text-[15px] font-semibold">
                You are near <StopName stop={{ position: located.value, place: Option.none() }} />
              </p>
              <DoneMark />
            </Sign>
          )
          : (
            <Sign tone="choice" className="items-center">
              <IconBubble>
                <LocateFixed />
              </IconBubble>
              <p className="min-w-0 flex-1 text-[15px]">
                You are outside Amsterdam, the one city Surge runs in, so you will type your pickup
                there.
              </p>
            </Sign>
          )
        : AsyncResult.isFailure(located) && !located.waiting && (
          <Sign tone="choice">
            <p className="text-[15px]">{whyNoPosition(located.cause)}</p>
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
    <motion.div
      aria-hidden
      initial={{ scale: 0.6, opacity: 0 }}
      animate={{ scale: 1, opacity: 1 }}
      transition={{ type: "spring", stiffness: 420, damping: 22 }}
      className="grid size-14 place-items-center rounded-full bg-success text-success-foreground"
    >
      <Check className="size-7" strokeWidth={3} />
    </motion.div>
    <StepHeader
      title="You are set"
      text={props.setup === "rider"
        ? "Choose where you are going, and see the price before you book."
        : "Go on shift from the Drive page, and offers start coming in."}
    />
    <ul className="flex flex-col gap-2">
      {props.done.map((item) => (
        <li key={item.label} className="flex items-center gap-3 rounded-2xl bg-tile px-4 py-3">
          <span
            aria-hidden
            className={cn(
              "grid size-6 shrink-0 place-items-center rounded-full",
              item.done
                ? "bg-success text-success-foreground"
                : "ring-2 ring-foreground/20 ring-inset",
            )}
          >
            {item.done && <Check className="size-3.5" strokeWidth={3} />}
          </span>
          <span className="flex-1 text-[15px] font-semibold">{item.label}</span>
          <span className="text-[13px] text-muted-foreground">
            {item.done ? "Done" : "Later, when you need it"}
          </span>
        </li>
      ))}
    </ul>
  </>
);
