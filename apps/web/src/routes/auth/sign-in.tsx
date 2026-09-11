import { sendCode, sessionAtom, signInWithGoogle, verifyCode } from "@/atom/session-atoms.js";
import { AuthCard, GoogleButton } from "@/components/auth/auth-card.js";
import { roleField } from "@/components/auth/role-field.js";
import { textField } from "@/components/auth/text-field.js";
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
import * as React from "react";

/**
 * Signing in and signing up are one page: an email address or a phone number,
 * then the code sent to it. The first code verified for an address makes the
 * account — with the role chosen here — so nobody has to know in advance which
 * of the two they are doing, and there is no password to forget.
 */

/** Where a code went, and the role an account made by it gets. */
interface Sent {
  readonly to: Contact;
  readonly role: SignUpRole;
}

type Method = Contact["kind"];

const ROLE_LEGEND = "New here? I want to";

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
      code: textField({ label: "Code", autoComplete: "one-time-code", inputMode: "numeric" }),
    },
    onSubmit: (sent: Sent, { decoded }) =>
      verifyCode({ to: sent.to, code: decoded.code, role: sent.role }),
  },
);

/** A new code for the same address, when the first did not arrive or ran out. */
const resendAtom = Atom.fn((to: Contact) => sendCode(to));

const MethodPicker = (
  props: { readonly method: Method; readonly onChange: (method: Method) => void; },
) => (
  <div role="group" aria-label="Send the code by" className="grid grid-cols-2 gap-2">
    {(["email", "phone"] as const).map((method) => (
      <Button
        key={method}
        type="button"
        variant={props.method === method ? "secondary" : "ghost"}
        aria-pressed={props.method === method}
        onClick={() => {
          props.onChange(method);
        }}
      >
        {method === "email" ? "Email" : "Phone"}
      </Button>
    ))}
  </div>
);

const EmailRequest = (props: { readonly onSent: (sent: Sent) => void; }) => {
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
      <div className="flex flex-col gap-4">
        <emailForm.email submitted={submitted} />
        <emailForm.role submitted={submitted} />
        {result._tag === "Failure" && (
          <Alert variant="destructive">
            <AlertDescription>
              {submitMessage(result, "We could not send a code there. Try again shortly.")}
            </AlertDescription>
          </Alert>
        )}
        <Button type="button" className="w-full" disabled={result.waiting} onClick={() => submit()}>
          {result.waiting ? "Sending…" : "Email me a code"}
        </Button>
      </div>
    </emailForm.Initialize>
  );
};

const PhoneRequest = (props: { readonly onSent: (sent: Sent) => void; }) => {
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
      <div className="flex flex-col gap-4">
        <phoneForm.phoneNumber submitted={submitted} />
        <phoneForm.role submitted={submitted} />
        {result._tag === "Failure" && (
          <Alert variant="destructive">
            <AlertDescription>
              {submitMessage(result, "We could not text a code there. Try again shortly.")}
            </AlertDescription>
          </Alert>
        )}
        <Button type="button" className="w-full" disabled={result.waiting} onClick={() => submit()}>
          {result.waiting ? "Sending…" : "Text me a code"}
        </Button>
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
    <p className="text-muted-foreground text-sm">
      Development build: the code is <span className="font-mono">{code.value.value}</span>.
    </p>
  );
};

const VerifyStep = (props: { readonly sent: Sent; readonly onBack: () => void; }) => {
  const submit = useAtomSet(codeForm.submit);
  const result = useAtomValue(codeForm.submit);
  const submitted = useAtomValue(codeForm.submitCount) > 0;
  const resend = useAtomSet(resendAtom, { mode: "promise" });
  const resending = useAtomValue(resendAtom);
  const address = addressOf(props.sent.to);
  const refreshDevCode = useAtomRefresh(devCode(address));
  const refresh = useAtomRefresh(sessionAtom);
  const navigate = useNavigate();

  React.useEffect(() => {
    if (result._tag !== "Success") return;
    refresh();
    void navigate({ to: "/" });
  }, [result, refresh, navigate]);

  return (
    <codeForm.Initialize defaultValues={{ code: "" }}>
      <div className="flex flex-col gap-4">
        <p className="text-muted-foreground text-sm">
          Enter the code sent to{" "}
          <span className="font-mono">{describeContact(props.sent.to)}</span>.
        </p>
        {import.meta.env.DEV && <DevCode address={address} />}
        <codeForm.code submitted={submitted} />
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
        <Button
          type="button"
          className="w-full"
          disabled={result.waiting}
          onClick={() => submit(props.sent)}
        >
          {result.waiting ? "Checking…" : "Continue"}
        </Button>

        {resending._tag === "Success" && !resending.waiting && (
          <p role="status" className="text-muted-foreground text-sm">A new code is on its way.</p>
        )}
        {resending._tag === "Failure" && (
          <Alert variant="destructive">
            <AlertDescription>
              {submitMessage(resending, "We could not send a new code. Try again shortly.")}
            </AlertDescription>
          </Alert>
        )}
        <div className="flex flex-wrap gap-x-4 gap-y-1">
          <Button
            type="button"
            variant="link"
            className="px-0"
            disabled={resending.waiting}
            onClick={() => {
              // A refused resend is shown from its own result, above.
              void resend(props.sent.to).then(refreshDevCode, () => {});
            }}
          >
            Send a new code
          </Button>
          <Button type="button" variant="link" className="px-0" onClick={props.onBack}>
            {props.sent.to.kind === "email" ? "Use a different email" : "Use a different number"}
          </Button>
        </div>
      </div>
    </codeForm.Initialize>
  );
};

const SignIn = () => {
  const [method, setMethod] = React.useState<Method>("email");
  const [sent, setSent] = React.useState<Sent | undefined>(undefined);

  return (
    <AuthCard
      title={sent === undefined ? "Sign in or create an account" : "Enter your code"}
      description={sent === undefined
        ? "We'll send you a six digit code. There is no password to remember."
        : "It expires in five minutes."}
    >
      {sent === undefined
        ? (
          <>
            <MethodPicker method={method} onChange={setMethod} />
            {method === "email"
              ? <EmailRequest onSent={setSent} />
              : <PhoneRequest onSent={setSent} />}
            <GoogleButton onClick={() => void Effect.runPromise(signInWithGoogle)} />
          </>
        )
        : (
          <VerifyStep
            sent={sent}
            onBack={() => {
              setSent(undefined);
            }}
          />
        )}
    </AuthCard>
  );
};

export const Route = createFileRoute("/auth/sign-in")({ component: SignIn });
