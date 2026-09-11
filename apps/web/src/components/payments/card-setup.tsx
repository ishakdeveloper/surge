import { confirmCardAtom } from "@/atom/payment-atoms.js";
import { ActionError } from "@/components/app/action-error.js";
import { actionVariants } from "@/components/sign/sign.js";
import { Button } from "@/components/ui/button.js";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog.js";
import { stripe } from "@/lib/stripe.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { Elements, PaymentElement, useElements, useStripe } from "@stripe/react-stripe-js";
import type { Appearance, Stripe } from "@stripe/stripe-js";
import { startCardSetup } from "@surge/common/atom/payment-atoms";
import { useBlocker } from "@tanstack/react-router";
import { Option } from "effect";
import { AsyncResult, Atom } from "effect/unstable/reactivity";
import { LoaderCircle } from "lucide-react";
import * as React from "react";

/**
 * Adding or replacing the rider's card, wherever it is asked for: the payment
 * page and the rider's onboarding. The card number itself never touches this
 * app or our servers — Stripe's Payment Element collects it in Stripe's own
 * frame, and Stripe tells the payments service by webhook.
 */
export const CardSetup = () =>
  Option.match(stripe, {
    onNone: () => <FakeCard />,
    onSome: (loading) => <StripeCard stripe={loading} />,
  });

/**
 * Stripe's form in the sheet's own terms: white fields with the tiles' rounded
 * corners, ink for focus, the refused red for errors. It renders in Stripe's
 * frame, which cannot load the self-hosted face, so it asks for the platform's
 * rounded one and falls back to the system sans.
 */
const appearance: Appearance = {
  theme: "flat",
  variables: {
    colorPrimary: "#1a1a1a",
    colorBackground: "#ffffff",
    colorText: "#1a1a1a",
    colorTextSecondary: "#5f636a",
    colorDanger: "#c8312a",
    fontFamily: "\"SF Pro Rounded\", ui-rounded, system-ui, sans-serif",
    borderRadius: "12px",
    spacingUnit: "4px",
  },
  rules: {
    ".Input": { border: "1px solid #e3e3df", boxShadow: "none" },
    ".Input:focus": { border: "1px solid #1a1a1a", boxShadow: "0 0 0 3px rgba(26, 26, 26, 0.08)" },
    ".Label": { fontWeight: "600", color: "#5f636a" },
  },
};

/**
 * No publishable key means the payments service runs its fake processor, and
 * there is no Stripe for a card form to talk to. The fake saves its test Visa
 * the moment it is asked, so this is a button rather than a form.
 */
const FakeCard = () => {
  const saving = useAtomValue(startCardSetup);
  const save = useAtomSet(startCardSetup);

  return (
    <>
      <p className="text-[15px] text-pretty text-muted-foreground">
        Payments are running against the fake processor, so there is no card form. This saves its
        test Visa ending 4242, which every hold succeeds on.
      </p>
      {AsyncResult.isFailure(saving) && <ActionError cause={saving.cause} />}
      <button
        type="button"
        disabled={saving.waiting}
        onClick={() => {
          save();
        }}
        className={actionVariants({ size: "block" })}
      >
        {saving.waiting && <LoaderCircle className="size-4 motion-safe:animate-spin" />}
        {saving.waiting ? "Saving…" : "Save the test card"}
      </button>
    </>
  );
};

/**
 * Against Stripe: ask the payments service for a SetupIntent, then mount
 * Stripe's form with its client secret. The form is keyed by that secret, so
 * replacing a card mounts a fresh one — Elements options cannot change once set.
 */
const StripeCard = (props: { readonly stripe: Promise<Stripe | null>; }) => {
  const setup = useAtomValue(startCardSetup);
  const start = useAtomSet(startCardSetup);

  if (AsyncResult.isSuccess(setup)) {
    const { clientSecret } = setup.value;
    return (
      <Elements key={clientSecret} stripe={props.stripe} options={{ clientSecret, appearance }}>
        <CardForm clientSecret={clientSecret} />
      </Elements>
    );
  }

  return (
    <>
      <p className="text-[15px] text-pretty text-muted-foreground">
        Stripe collects the card in its own form, so the number never reaches Surge.
      </p>
      {AsyncResult.isFailure(setup) && <ActionError cause={setup.cause} />}
      <button
        type="button"
        disabled={setup.waiting}
        onClick={() => {
          start();
        }}
        className={actionVariants({ size: "block" })}
      >
        {setup.waiting && <LoaderCircle className="size-4 motion-safe:animate-spin" />}
        {setup.waiting ? "Opening the card form…" : "Add or replace your card"}
      </button>
    </>
  );
};

const CardForm = (props: { readonly clientSecret: string; }) => {
  const stripe = useStripe();
  const elements = useElements();
  const confirming = useAtomValue(confirmCardAtom(props.clientSecret));
  const confirm = useAtomSet(confirmCardAtom(props.clientSecret));
  const close = useAtomSet(startCardSetup);
  const [entered, setEntered] = React.useState(false);

  const saved = AsyncResult.isSuccess(confirming);
  // Card details typed and not yet with Stripe are the one thing on this page
  // that leaving would lose.
  const unsaved = entered && !saved;
  const blocker = useBlocker({
    shouldBlockFn: () => unsaved,
    enableBeforeUnload: () => unsaved,
    withResolver: true,
  });

  if (saved) {
    return (
      <output className="text-[15px] motion-safe:animate-rise-in">
        Stripe has your card. It shows above as soon as Stripe confirms it to us, usually within a
        few seconds.
      </output>
    );
  }

  return (
    <form
      noValidate
      className="flex flex-col gap-3"
      onSubmit={(event) => {
        event.preventDefault();
        if (stripe !== null && elements !== null) confirm({ stripe, elements });
      }}
    >
      {/* Stripe's own frame, which validates and labels its fields itself: white fields on a tile. */}
      <div className="rounded-2xl bg-tile p-3">
        <PaymentElement
          onChange={(event) => {
            setEntered(!event.empty);
          }}
        />
      </div>
      {AsyncResult.isFailure(confirming) && <ActionError cause={confirming.cause} />}
      <div className="flex flex-col gap-1">
        <button
          type="submit"
          disabled={stripe === null || elements === null || confirming.waiting}
          className={actionVariants({ size: "block" })}
        >
          {confirming.waiting && <LoaderCircle className="size-4 motion-safe:animate-spin" />}
          {confirming.waiting ? "Saving…" : "Save card"}
        </button>
        <button
          type="button"
          disabled={confirming.waiting}
          onClick={() => {
            close(Atom.Reset);
          }}
          className={actionVariants({ tone: "quiet", size: "block" })}
        >
          Cancel
        </button>
      </div>

      <Dialog
        open={blocker.status === "blocked"}
        onOpenChange={(open) => {
          if (!open) blocker.reset?.();
        }}
      >
        <DialogContent showCloseButton={false}>
          <DialogHeader>
            <DialogTitle>Leave without saving your card?</DialogTitle>
            <DialogDescription>
              The card details you entered have not been sent to Stripe, and leaving discards them.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => blocker.reset?.()}>
              Stay
            </Button>
            <Button type="button" variant="destructive" onClick={() => blocker.proceed?.()}>
              Leave
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </form>
  );
};
