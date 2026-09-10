import { confirmCardAtom, paymentPushesAtom, startCardSetup } from "@/atom/payment-atoms.js";
import { sessionAtom } from "@/atom/session-atoms.js";
import { ActionError } from "@/components/app/action-error.js";
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
import { CardSummary } from "@/routes/_protected/ride/-components/card-summary.js";
import { useAtomMount, useAtomSet, useAtomValue } from "@effect/atom-react";
import { Elements, PaymentElement, useElements, useStripe } from "@stripe/react-stripe-js";
import type { Stripe } from "@stripe/stripe-js";
import { createFileRoute, Link, useBlocker } from "@tanstack/react-router";
import { Option } from "effect";
import { AsyncResult, Atom } from "effect/unstable/reactivity";
import * as React from "react";

/**
 * The rider's card: the one every hold is placed on.
 *
 * A fare is held on it when a trip is booked and charged when the trip ends;
 * a cancelled or unmatched trip releases the hold. The card number itself
 * never touches this app or our servers — Stripe's Payment Element collects
 * it in Stripe's own frame, and Stripe tells the payments service by webhook.
 */
const PaymentPage = () => {
  useAtomMount(paymentPushesAtom);
  const role = useAtomValue(
    sessionAtom,
    (session) => AsyncResult.isSuccess(session) ? session.value.role : undefined,
  );

  if (role === "driver") {
    return (
      <p className="text-muted-foreground text-sm">
        This page is for riders. Your account drives — your pay is on the{" "}
        <Link to="/drive/earnings" className="underline underline-offset-4">Earnings</Link> page.
      </p>
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-6 overflow-y-auto">
      <header className="flex flex-col gap-1">
        <h1 className="text-lg font-semibold">Payment</h1>
        <p className="text-muted-foreground text-sm">
          Your fare is held on this card when you book and charged when the trip ends. A cancelled
          trip releases the hold.
        </p>
      </header>

      <section
        className="flex max-w-xl flex-col gap-3 rounded-md border border-border p-4"
        aria-labelledby="card"
      >
        <h2 id="card" className="text-sm font-medium">Card</h2>
        <CardSummary />
        {Option.match(stripe, {
          onNone: () => <FakeCard />,
          onSome: (loading) => <StripeCard stripe={loading} />,
        })}
      </section>
    </div>
  );
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
      <p className="text-muted-foreground text-sm">
        Payments are running against the fake processor, so there is no card form. This saves its
        test Visa ending 4242, which every hold succeeds on.
      </p>
      {AsyncResult.isFailure(saving) && <ActionError cause={saving.cause} />}
      <div>
        <Button
          type="button"
          disabled={saving.waiting}
          onClick={() => {
            save();
          }}
        >
          {saving.waiting ? "Saving…" : "Save the test card"}
        </Button>
      </div>
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
      <Elements key={clientSecret} stripe={props.stripe} options={{ clientSecret }}>
        <CardForm clientSecret={clientSecret} />
      </Elements>
    );
  }

  return (
    <>
      {AsyncResult.isFailure(setup) && <ActionError cause={setup.cause} />}
      <div>
        <Button
          type="button"
          disabled={setup.waiting}
          onClick={() => {
            start();
          }}
        >
          {setup.waiting ? "Opening the card form…" : "Add or replace your card"}
        </Button>
      </div>
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
      <output className="text-sm">
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
      {/* Stripe's own frame, which validates and labels its fields itself. */}
      <PaymentElement
        onChange={(event) => {
          setEntered(!event.empty);
        }}
      />
      {AsyncResult.isFailure(confirming) && <ActionError cause={confirming.cause} />}
      <div className="flex gap-2">
        <Button type="submit" disabled={stripe === null || elements === null || confirming.waiting}>
          {confirming.waiting ? "Saving…" : "Save card"}
        </Button>
        <Button
          type="button"
          variant="outline"
          disabled={confirming.waiting}
          onClick={() => {
            close(Atom.Reset);
          }}
        >
          Cancel
        </Button>
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

export const Route = createFileRoute("/_protected/ride/payment")({
  // Client-only: Stripe.js runs in the browser and nowhere else.
  ssr: false,
  staticData: { crumb: "Payment" },
  component: PaymentPage,
});
