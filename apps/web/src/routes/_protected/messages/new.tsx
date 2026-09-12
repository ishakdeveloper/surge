import { sessionAtom } from "@/atom/session-atoms.js";
import { ActionError } from "@/components/app/action-error.js";
import { QueryError } from "@/components/app/query-error.js";
import { textField } from "@/components/auth/text-field.js";
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
import { Label } from "@/components/ui/label.js";
import { Textarea } from "@/components/ui/textarea.js";
import { cn } from "@/lib/utils.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { FormBuilder, FormReact } from "@lucas-barake/effect-form-react";
import { SurgeApi } from "@surge/client/SurgeApi";
import { Keys } from "@surge/common/atom/reactivity-keys";
import { runtime } from "@surge/common/atom/runtime";
import { tripsAtom } from "@surge/common/atom/trip-atoms";
import { SupportBody, SupportSubject } from "@surge/common/chat/support";
import { driverStatus, formatCents, formatDistance, riderStatus } from "@surge/common/lib/format";
import { TripId } from "@surge/domain/api/Primitives";
import { createFileRoute, Link, useBlocker, useNavigate } from "@tanstack/react-router";
import { Cause, Effect, Option, Schema } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";
import { ArrowLeft, ChevronDown, LoaderCircle } from "lucide-react";
import { motion } from "motion/react";
import * as React from "react";

/**
 * Asking Surge support for help: what it is about, which trip if any, and what
 * happened. Sending opens the conversation, where support replies.
 */

/** What effect-form hands a field renderer; the same shape `textField` takes. */
interface FieldProps {
  readonly field: {
    readonly value: string;
    readonly onChange: (value: string) => void;
    readonly onBlur: () => void;
    readonly error: Option.Option<string>;
    readonly isDirty: boolean;
  };
  readonly props: { readonly submitted?: boolean; };
}

/** Hidden until the field has been typed in or a submit was tried, as `textField` does. */
const shownError = (props: FieldProps) =>
  props.field.isDirty || props.props.submitted === true ? props.field.error : Option.none();

const FieldError = (props: { readonly id: string; readonly message: string; }) => (
  <motion.p
    id={props.id}
    initial={{ opacity: 0, y: -4 }}
    animate={{ opacity: 1, y: 0 }}
    transition={{ duration: 0.16 }}
    className="text-[13px] font-medium text-destructive"
  >
    {props.message}
  </motion.p>
);

/**
 * Which of the caller's trips it is about, if any. Optional, so it has no
 * error of its own; a trip list that did not load leaves "not about a trip"
 * and says why.
 */
const TripField = (props: FieldProps) => {
  const id = React.useId();
  const trips = useAtomValue(tripsAtom);
  const role = useAtomValue(
    sessionAtom,
    (session) => AsyncResult.isSuccess(session) ? session.value.role : undefined,
  );
  const status = role === "driver" ? driverStatus : riderStatus;

  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={id}>
        About a trip <span className="font-normal">(optional)</span>
      </Label>
      <div className="relative">
        <select
          id={id}
          value={props.field.value}
          onChange={(event) => {
            props.field.onChange(event.target.value);
          }}
          onBlur={props.field.onBlur}
          disabled={!AsyncResult.isSuccess(trips)}
          className="h-12 w-full cursor-pointer appearance-none rounded-2xl bg-tile pr-11 pl-4 text-[15px] outline-none transition-[background-color,box-shadow] duration-150 ease-out focus:bg-card focus:shadow-[inset_0_0_0_2px_var(--foreground)] disabled:cursor-not-allowed disabled:opacity-60"
        >
          <option value="">Not about a trip</option>
          {AsyncResult.isSuccess(trips)
            && trips.value.slice(0, 12).map((trip) => (
              <option key={trip.id} value={trip.id}>
                {status[trip.status]} · {formatCents(trip.totalCents)} ·{" "}
                {formatDistance(trip.route.meters)}
              </option>
            ))}
        </select>
        <ChevronDown
          aria-hidden
          className="pointer-events-none absolute top-1/2 right-4 size-4 -translate-y-1/2 text-muted-foreground"
        />
      </div>
      {AsyncResult.isFailure(trips) && <QueryError result={trips} subject="your trips" />}
    </div>
  );
};

/** What happened: a grey field, with a hint that gives way to the error when there is one. */
const BodyField = (props: FieldProps) => {
  const id = React.useId();
  const errorId = `${id}-error`;
  const hintId = `${id}-hint`;
  const error = shownError(props);

  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={id}>What happened?</Label>
      <Textarea
        id={id}
        rows={5}
        maxLength={1000}
        // The browser's grip fights the rounded field; five rows is room enough.
        className="resize-none"
        value={props.field.value}
        onChange={(event) => {
          props.field.onChange(event.target.value);
        }}
        onBlur={props.field.onBlur}
        aria-invalid={Option.isSome(error)}
        aria-describedby={Option.isSome(error) ? errorId : hintId}
      />
      {Option.isSome(error)
        ? <FieldError id={errorId} message={error.value} />
        : (
          <p id={hintId} className="text-[13px] text-muted-foreground">
            When and where it happened helps support find it quickly.
          </p>
        )}
    </div>
  );
};

/**
 * The request, sent the way `contactSupport` sends it. The form owns the
 * call rather than handing off to that atom, so its submit state — waiting,
 * refused, done — is one thing the page reads.
 */
const supportForm = FormReact.make(
  FormBuilder.empty
    .addField("subject", SupportSubject)
    .addField("tripId", Schema.String)
    .addField("body", SupportBody),
  {
    runtime,
    mode: { validation: "onBlur" },
    reactivityKeys: [Keys.chat],
    fields: {
      subject: textField({ label: "What is it about?" }),
      tripId: TripField,
      body: BodyField,
    },
    onSubmit: (idempotencyKey: string, { decoded }) =>
      Effect.gen(function*() {
        const api = yield* SurgeApi;
        const { conversation } = yield* api.support.create({
          payload: {
            tripId: TripId.make(decoded.tripId),
            subject: decoded.subject.trim(),
            body: decoded.body.trim(),
            idempotencyKey,
          },
        });
        return conversation;
      }),
  },
);

/** Whether a refused submit was the form's own validation, which each field already shows. */
const isValidation = (cause: Cause.Cause<unknown>): boolean => {
  const error = Option.getOrUndefined(Cause.findErrorOption(cause));
  return typeof error === "object" && error !== null && "_tag" in error
    && error._tag === "SchemaError";
};

// A click handler needs the key now, not an Effect to run for it; it only has
// to be unique, which this is.
// oxlint-disable-next-line effecttsgo/crypto-random-uuid
const newKey = () => crypto.randomUUID();

const ContactSupport = () => {
  const submit = useAtomSet(supportForm.submit);
  const result = useAtomValue(supportForm.submit);
  const submitted = useAtomValue(supportForm.submitCount) > 0;
  const dirty = useAtomValue(supportForm.isDirty);
  const navigate = useNavigate();
  // One key for this request, kept across retries: pressing Send again after a
  // failure is the same request, and support sees one conversation.
  const [key] = React.useState(newKey);
  const sent = AsyncResult.isSuccess(result);

  React.useEffect(() => {
    if (!AsyncResult.isSuccess(result)) return;
    void navigate({
      to: "/messages/$conversationId",
      params: { conversationId: result.value.id },
      replace: true,
    });
  }, [result, navigate]);

  // What was typed and not sent is the one thing leaving would lose.
  const unsent = dirty && !sent;
  const blocker = useBlocker({
    shouldBlockFn: () => unsent,
    enableBeforeUnload: () => unsent,
    withResolver: true,
  });

  // A refused submit that is the form's own validation already shows at each
  // field; anything else is the server's answer, and shows here.
  const refused = AsyncResult.isFailure(result) && !isValidation(result.cause)
    ? Option.some(result.cause)
    : Option.none();

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
      <div className="mx-auto flex w-full max-w-md flex-col gap-5 rounded-3xl bg-card p-5 shadow-[0_1px_2px_rgb(0_0_0/0.04),0_12px_32px_-18px_rgb(0_0_0/0.22)]">
        <header className="flex items-start gap-3">
          <Link
            to="/messages"
            aria-label="Back to messages"
            className="mt-0.5 grid size-9 shrink-0 place-items-center rounded-full bg-tile transition-colors duration-150 hover:bg-tile-hover"
          >
            <ArrowLeft aria-hidden className="size-4" />
          </Link>
          <div className="flex flex-col gap-1">
            <h1 className="text-2xl leading-tight font-bold">Contact support</h1>
            <p className="text-[15px] text-pretty text-muted-foreground">
              Someone from Surge replies in the conversation this opens. You can follow it from
              Messages.
            </p>
          </div>
        </header>

        <supportForm.Initialize defaultValues={{ subject: "", tripId: "", body: "" }}>
          <form
            noValidate
            className="flex flex-col gap-5"
            onSubmit={(event) => {
              event.preventDefault();
              submit(key);
            }}
          >
            <supportForm.subject submitted={submitted} />
            <supportForm.tripId submitted={submitted} />
            <supportForm.body submitted={submitted} />
            {Option.isSome(refused) && <ActionError cause={refused.value} />}
            <button
              type="submit"
              disabled={result.waiting}
              className={cn(actionVariants({ size: "block" }), "h-14 text-[17px] font-bold")}
            >
              {result.waiting && <LoaderCircle className="size-5 motion-safe:animate-spin" />}
              {result.waiting ? "Sending…" : "Send to support"}
            </button>
          </form>
        </supportForm.Initialize>

        <Dialog
          open={blocker.status === "blocked"}
          onOpenChange={(open) => {
            if (!open) blocker.reset?.();
          }}
        >
          <DialogContent showCloseButton={false}>
            <DialogHeader>
              <DialogTitle>Leave without sending?</DialogTitle>
              <DialogDescription>
                What you wrote has not been sent to support, and leaving discards it.
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
      </div>
    </div>
  );
};

export const Route = createFileRoute("/_protected/messages/new")({
  ssr: false,
  staticData: { crumb: "Contact support" },
  component: ContactSupport,
});
