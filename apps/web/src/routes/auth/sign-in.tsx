import { sendCode, sessionAtom, signInWithGoogle, verifyCode } from "@/atom/session-atoms.js";
import { AuthCard, GoogleButton } from "@/components/auth/auth-card.js";
import { roleField } from "@/components/auth/role-field.js";
import { codeField, textField } from "@/components/auth/text-field.js";
import { Alert, AlertDescription } from "@/components/ui/alert.js";
import { Button } from "@/components/ui/button.js";
import { authBaseUrl } from "@/iam/auth-client.js";
import { useAtomRefresh, useAtomSet, useAtomValue } from "@effect/atom-react";
import { FormBuilder, FormReact } from "@lucas-barake/effect-form-react";
import { submitMessage } from "@surge/common/iam/auth-result";
import {
  Email,
  normalizePhoneNumber,
  OtpCode,
  PhoneNumber,
  SignUpRole,
} from "@surge/common/iam/auth-schemas";
import { addressOf, devCodeFamily } from "@surge/common/iam/dev-code";
import { type Contact, describeContact } from "@surge/domain/iam/Contact";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { Effect, Option } from "effect";
import { AsyncResult, Atom } from "effect/unstable/reactivity";
import { LoaderCircle } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import * as React from "react";

/**
 * Signing in and signing up are one page: an email address — or, offered
 * quietly, a phone number — then the code sent to it. The first code verified
 * for an address makes the account, with the role chosen here, so nobody has
 * to know in advance which of the two they are doing, and there is no
 * password to forget.
 */

/** Where a code went, and the role an account made by it gets. */
interface Sent {
  readonly to: Contact;
  readonly role: SignUpRole;
}

type Step = { readonly kind: "email"; } | { readonly kind: "phone"; } | {
  readonly kind: "verify";
  readonly sent: Sent;
};

const ROLE_LEGEND = "New here? I want to";

const EASE_OUT = [0.22, 1, 0.36, 1] as const;

const emailForm = FormReact.make(
  FormBuilder.empty.addField("email", Email).addField("role", SignUpRole),
  {
    mode: { validation: "onBlur" },
    fields: {
      email: textField({ label: "Email", type: "email", autoComplete: "email" }),
      role: roleField({ legend: ROLE_LEGEND }),
    },
    onSubmit: (_, { decoded }) => sendCode({ kind: "email", email: decoded.email }),
  },
);

const phoneForm = FormReact.make(
  FormBuilder.empty.addField("phoneNumber", PhoneNumber).addField("role", SignUpRole),
  {
    mode: { validation: "onBlur" },
    fields: {
      phoneNumber: textField({ label: "Phone number", type: "tel", autoComplete: "tel" }),
      role: roleField({ legend: ROLE_LEGEND }),
    },
    onSubmit: (_, { decoded }) =>
      sendCode({ kind: "phone", phoneNumber: normalizePhoneNumber(decoded.phoneNumber) }),
  },
);

const codeForm = FormReact.make(
  FormBuilder.empty.addField("code", OtpCode),
  {
    mode: { validation: "onBlur" },
    fields: {
      code: codeField({ label: "Six-digit code" }),
    },
    onSubmit: (sent: Sent, { decoded }) =>
      verifyCode({ to: sent.to, code: decoded.code, role: sent.role }),
  },
);

/** A new code for the same address, when the first did not arrive or ran out. */
const resendAtom = Atom.fn((to: Contact) => sendCode(to));

/** The primary action, with a spinner while it is working. */
const Submit = (
  props: { readonly waiting: boolean; readonly label: string; readonly onClick: () => void; },
) => (
  <Button
    type="button"
    size="lg"
    className="w-full"
    disabled={props.waiting}
    onClick={props.onClick}
  >
    {props.waiting && <LoaderCircle className="motion-safe:animate-spin" />}
    {props.label}
  </Button>
);

/**
 * A quiet way to somewhere else on this sheet: small, grey, centred under the
 * action with air above it, so it reads as an aside rather than a second
 * choice. The link colours in on hover; the underline is for keyboard focus.
 */
const Aside = (props: { readonly children: React.ReactNode; }) => (
  <p className="text-center text-[13px] leading-relaxed text-muted-foreground/80">
    {props.children}
  </p>
);

const AsideLink = (props: {
  readonly disabled?: boolean;
  readonly onClick: () => void;
  readonly children: React.ReactNode;
}) => (
  <button
    type="button"
    disabled={props.disabled}
    onClick={props.onClick}
    className="cursor-pointer rounded-sm font-medium text-muted-foreground underline-offset-4 transition-colors duration-150 hover:text-foreground focus-visible:underline disabled:cursor-not-allowed disabled:opacity-50"
  >
    {props.children}
  </button>
);

const EmailRequest = (
  props: { readonly onSent: (sent: Sent) => void; readonly onPhone: () => void; },
) => {
  const submit = useAtomSet(emailForm.submit);
  const result = useAtomValue(emailForm.submit);
  // Reveal errors from the first submit attempt, not just failed ones.
  const submitted = useAtomValue(emailForm.submitCount) > 0;
  const values = useAtomValue(emailForm.values);

  React.useEffect(() => {
    if (result._tag !== "Success" || values._tag !== "Some") return;
    props.onSent({ to: { kind: "email", email: values.value.email }, role: values.value.role });
  }, [result, values, props]);

  return (
    <emailForm.Initialize defaultValues={{ email: "", role: "rider" }}>
      <div className="flex flex-col gap-5">
        <emailForm.email submitted={submitted} />
        <emailForm.role submitted={submitted} />
        {result._tag === "Failure" && (
          <Alert variant="destructive">
            <AlertDescription>
              {submitMessage(result, "We could not send a code there. Try again shortly.")}
            </AlertDescription>
          </Alert>
        )}
        <Submit
          waiting={result.waiting}
          label={result.waiting ? "Sending…" : "Email me a code"}
          onClick={() => submit()}
        />
        <div className="pt-1">
          <Aside>
            No email to hand? <AsideLink onClick={props.onPhone}>Text it to your phone</AsideLink>
          </Aside>
        </div>
      </div>
    </emailForm.Initialize>
  );
};

const PhoneRequest = (
  props: { readonly onSent: (sent: Sent) => void; readonly onEmail: () => void; },
) => {
  const submit = useAtomSet(phoneForm.submit);
  const result = useAtomValue(phoneForm.submit);
  const submitted = useAtomValue(phoneForm.submitCount) > 0;
  const values = useAtomValue(phoneForm.values);

  React.useEffect(() => {
    if (result._tag !== "Success" || values._tag !== "Some") return;
    props.onSent({
      to: { kind: "phone", phoneNumber: normalizePhoneNumber(values.value.phoneNumber) },
      role: values.value.role,
    });
  }, [result, values, props]);

  return (
    <phoneForm.Initialize defaultValues={{ phoneNumber: "", role: "rider" }}>
      <div className="flex flex-col gap-5">
        <phoneForm.phoneNumber submitted={submitted} />
        <phoneForm.role submitted={submitted} />
        {result._tag === "Failure" && (
          <Alert variant="destructive">
            <AlertDescription>
              {submitMessage(result, "We could not text a code there. Try again shortly.")}
            </AlertDescription>
          </Alert>
        )}
        <Submit
          waiting={result.waiting}
          label={result.waiting ? "Sending…" : "Text me a code"}
          onClick={() => submit()}
        />
        <div className="pt-1">
          <Aside>
            <AsideLink onClick={props.onEmail}>Use your email instead</AsideLink>
          </Aside>
        </div>
      </div>
    </phoneForm.Initialize>
  );
};

/** The code the auth service's dev outbox holds, for a development build to show. */
const devCode = devCodeFamily(authBaseUrl);

/**
 * In a development build, the code this step is waiting for — so a demo signs
 * in with no email or SMS provider, and nobody reads the auth service's log.
 * Rendered only when Vite builds for development; and nothing at all when auth
 * runs without its dev outbox.
 */
const DevCode = (props: { readonly address: string; }) => {
  const code = useAtomValue(devCode(props.address));
  if (!AsyncResult.isSuccess(code) || Option.isNone(code.value)) return null;

  return (
    <p className="rounded-2xl bg-tile px-4 py-3 text-sm text-muted-foreground">
      Development build: the code is{" "}
      <span className="font-bold tracking-widest text-foreground tabular-nums">
        {code.value.value}
      </span>
    </p>
  );
};

/**
 * The code. Filling the last box submits by itself, so the button under it is
 * for a code typed and then corrected. Under that, the ways out: a new code
 * to the same address, the other kind of address, or a different one.
 */
const VerifyStep = (props: {
  readonly sent: Sent;
  readonly onSwitch: (to: "email" | "phone") => void;
}) => {
  const submit = useAtomSet(codeForm.submit);
  const result = useAtomValue(codeForm.submit);
  const submitted = useAtomValue(codeForm.submitCount) > 0;
  const resend = useAtomSet(resendAtom, { mode: "promise" });
  const resending = useAtomValue(resendAtom);
  const address = addressOf(props.sent.to);
  const refreshDevCode = useAtomRefresh(devCode(address));
  const refresh = useAtomRefresh(sessionAtom);
  const navigate = useNavigate();
  const byEmail = props.sent.to.kind === "email";

  React.useEffect(() => {
    if (result._tag !== "Success") return;
    refresh();
    void navigate({ to: "/" });
  }, [result, refresh, navigate]);

  return (
    <codeForm.Initialize defaultValues={{ code: "" }}>
      <div className="flex flex-col gap-5">
        <p className="text-[15px] text-muted-foreground">
          We sent it to{" "}
          <span className="font-semibold text-foreground">{describeContact(props.sent.to)}</span>.
        </p>
        {import.meta.env.DEV && <DevCode address={address} />}
        <codeForm.code
          submitted={submitted}
          refused={result._tag === "Failure"}
          onComplete={() => submit(props.sent)}
        />
        {result._tag === "Failure" && (
          <Alert variant="destructive">
            <AlertDescription>
              {submitMessage(
                result,
                "That code is wrong or has expired. Send a new one if it has.",
              )}
            </AlertDescription>
          </Alert>
        )}
        <Submit
          waiting={result.waiting}
          label={result.waiting ? "Checking…" : "Continue"}
          onClick={() => submit(props.sent)}
        />

        {resending._tag === "Success" && !resending.waiting && (
          <p role="status" className="text-center text-sm text-muted-foreground">
            A new code is on its way.
          </p>
        )}
        {resending._tag === "Failure" && (
          <Alert variant="destructive">
            <AlertDescription>
              {submitMessage(resending, "We could not send a new code. Try again shortly.")}
            </AlertDescription>
          </Alert>
        )}
        <div className="flex flex-col gap-1 pt-3">
          <Aside>
            Nothing yet?{" "}
            <AsideLink
              disabled={resending.waiting}
              onClick={() => {
                // A refused resend is shown from its own result, above.
                void resend(props.sent.to).then(refreshDevCode, () => {});
              }}
            >
              Send a new code
            </AsideLink>
          </Aside>
          <Aside>
            <AsideLink onClick={() => props.onSwitch(byEmail ? "phone" : "email")}>
              {byEmail ? "Or text it to your phone" : "Or email it instead"}
            </AsideLink>
            {" · "}
            <AsideLink onClick={() => props.onSwitch(byEmail ? "email" : "phone")}>
              {byEmail ? "Use a different email" : "Use a different number"}
            </AsideLink>
          </Aside>
        </div>
      </div>
    </codeForm.Initialize>
  );
};

/** A step of the sheet sliding in from the side it moves towards. */
const Slide = (props: { readonly direction: 1 | -1; readonly children: React.ReactNode; }) => (
  <motion.div
    initial={{ opacity: 0, x: 24 * props.direction }}
    animate={{ opacity: 1, x: 0 }}
    exit={{ opacity: 0, x: -24 * props.direction }}
    transition={{ duration: 0.24, ease: EASE_OUT }}
  >
    {props.children}
  </motion.div>
);

const SignIn = () => {
  const [step, setStep] = React.useState<Step>({ kind: "email" });
  const verifying = step.kind === "verify";
  const onSent = (sent: Sent) => {
    setStep({ kind: "verify", sent });
  };

  return (
    <AuthCard
      title={verifying ? "Enter your code" : "Welcome to Surge"}
      description={verifying
        ? "It expires in five minutes."
        : "Sign in or create an account with a six-digit code. There is no password to remember."}
    >
      <AnimatePresence mode="wait" initial={false}>
        {step.kind === "email" && (
          <Slide key="email" direction={-1}>
            <div className="flex flex-col gap-5">
              <EmailRequest
                onSent={onSent}
                onPhone={() => {
                  setStep({ kind: "phone" });
                }}
              />
              <div
                aria-hidden
                className="flex items-center gap-3 text-xs font-semibold text-muted-foreground"
              >
                <span className="h-px flex-1 bg-border" />
                or
                <span className="h-px flex-1 bg-border" />
              </div>
              <GoogleButton onClick={() => void Effect.runPromise(signInWithGoogle)} />
            </div>
          </Slide>
        )}
        {step.kind === "phone" && (
          <Slide key="phone" direction={1}>
            <PhoneRequest
              onSent={onSent}
              onEmail={() => {
                setStep({ kind: "email" });
              }}
            />
          </Slide>
        )}
        {step.kind === "verify" && (
          <Slide key="verify" direction={1}>
            <VerifyStep
              sent={step.sent}
              onSwitch={(kind) => {
                setStep({ kind });
              }}
            />
          </Slide>
        )}
      </AnimatePresence>
    </AuthCard>
  );
};

export const Route = createFileRoute("/auth/sign-in")({ component: SignIn });
