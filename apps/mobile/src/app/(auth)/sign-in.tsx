import { sendCode, verifyCode } from "@/atom/session-atoms.js";
import { AuthScreen } from "@/components/auth/auth-screen.js";
import { roleField } from "@/components/auth/role-field.js";
import { textField } from "@/components/auth/text-field.js";
import { Alert, AlertDescription } from "@/components/ui/alert.js";
import { Button } from "@/components/ui/button.js";
import { Text } from "@/components/ui/text.js";
import { useRestartSession } from "@/iam/session-scope.js";
import { serviceUrls } from "@/lib/config.js";
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
import { Option } from "effect";
import { AsyncResult, Atom } from "effect/unstable/reactivity";
import * as React from "react";
import { View } from "react-native";

/**
 * Signing in and signing up are one screen, as on the web: an email address or
 * a phone number, then the code sent to it. The first code verified makes the
 * account, with the role chosen here, and there is no password to forget.
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
      email: textField({ label: "Email", kind: "email" }),
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
      phoneNumber: textField({ label: "Phone number", kind: "phone" }),
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
    fields: { code: textField({ label: "Code", kind: "code" }) },
    onSubmit: (sent: Sent, { decoded }) =>
      verifyCode({ to: sent.to, code: decoded.code, role: sent.role }),
  },
);

/** A new code for the same address, when the first did not arrive or ran out. */
const resendAtom = Atom.fn((to: Contact) => sendCode(to));

/** The code the auth service's dev outbox holds, for a development build to show. */
const devCode = devCodeFamily(serviceUrls.AUTH_BASE_URL);

const MethodPicker = (
  props: { readonly method: Method; readonly onChange: (method: Method) => void; },
) => (
  <View
    accessibilityRole="radiogroup"
    accessibilityLabel="Send the code by"
    className="flex-row gap-2"
  >
    {(["email", "phone"] as const).map((method) => (
      <Button
        key={method}
        testID={`method-${method}`}
        className="flex-1"
        variant={props.method === method ? "secondary" : "ghost"}
        accessibilityRole="radio"
        accessibilityState={{ checked: props.method === method }}
        onPress={() => {
          props.onChange(method);
        }}
      >
        {method === "email" ? "Email" : "Phone"}
      </Button>
    ))}
  </View>
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
      <View className="gap-4">
        <emailForm.email submitted={submitted} />
        <emailForm.role submitted={submitted} />
        {result._tag === "Failure" && (
          <Alert variant="destructive">
            <AlertDescription className="text-destructive">
              {submitMessage(result, "We could not send a code there. Try again shortly.")}
            </AlertDescription>
          </Alert>
        )}
        <Button
          disabled={result.waiting}
          onPress={() => {
            submit();
          }}
        >
          {result.waiting ? "Sending…" : "Email me a code"}
        </Button>
      </View>
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
      <View className="gap-4">
        <phoneForm.phoneNumber submitted={submitted} />
        <phoneForm.role submitted={submitted} />
        {result._tag === "Failure" && (
          <Alert variant="destructive">
            <AlertDescription className="text-destructive">
              {submitMessage(result, "We could not text a code there. Try again shortly.")}
            </AlertDescription>
          </Alert>
        )}
        <Button
          disabled={result.waiting}
          onPress={() => {
            submit();
          }}
        >
          {result.waiting ? "Sending…" : "Text me a code"}
        </Button>
      </View>
    </phoneForm.Initialize>
  );
};

/**
 * In a development build, the code this step is waiting for — so a demo signs
 * in by phone with no SMS provider, and nobody reads the auth service's log.
 * Rendered only under `__DEV__`, which a release build compiles away; and
 * nothing at all when auth runs without its dev outbox.
 */
const DevCode = (props: { readonly address: string; }) => {
  const code = useAtomValue(devCode(props.address));
  if (!AsyncResult.isSuccess(code) || Option.isNone(code.value)) return null;

  return (
    <Text className="text-muted-foreground">
      Development build: the code is <Text className="font-mono">{code.value.value}</Text>.
    </Text>
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
  const restart = useRestartSession();

  /**
   * The cookie is in SecureStore once this succeeds. Restarting the scope
   * reads the session afresh, and the navigator swaps to the app on its own.
   */
  React.useEffect(() => {
    if (result._tag === "Success") restart();
  }, [result, restart]);

  return (
    <codeForm.Initialize defaultValues={{ code: "" }}>
      <View className="gap-4">
        <Text className="text-muted-foreground">
          Enter the code sent to{" "}
          <Text className="font-mono">{describeContact(props.sent.to)}</Text>. It expires in five
          minutes.
        </Text>
        {__DEV__ && <DevCode address={address} />}
        <codeForm.code submitted={submitted} />
        {result._tag === "Failure" && (
          <Alert variant="destructive">
            <AlertDescription className="text-destructive">
              {submitMessage(
                result,
                "That code is wrong or has expired. Send a new one if it has.",
              )}
            </AlertDescription>
          </Alert>
        )}
        <Button
          disabled={result.waiting}
          onPress={() => {
            submit(props.sent);
          }}
        >
          {result.waiting ? "Checking…" : "Continue"}
        </Button>

        {resending._tag === "Success" && !resending.waiting && (
          <Text accessibilityLiveRegion="polite" className="text-muted-foreground">
            A new code is on its way.
          </Text>
        )}
        {resending._tag === "Failure" && (
          <Alert variant="destructive">
            <AlertDescription className="text-destructive">
              {submitMessage(resending, "We could not send a new code. Try again shortly.")}
            </AlertDescription>
          </Alert>
        )}
        <View className="flex-row gap-2">
          <Button
            className="flex-1"
            variant="ghost"
            disabled={resending.waiting}
            onPress={() => {
              // A refused resend is shown from its own result, above.
              void resend(props.sent.to).then(refreshDevCode, () => {});
            }}
          >
            Send a new code
          </Button>
          <Button className="flex-1" variant="ghost" onPress={props.onBack}>
            {props.sent.to.kind === "email" ? "Different email" : "Different number"}
          </Button>
        </View>
      </View>
    </codeForm.Initialize>
  );
};

const SignIn = () => {
  const [method, setMethod] = React.useState<Method>("email");
  const [sent, setSent] = React.useState<Sent | undefined>(undefined);

  if (sent !== undefined) {
    return (
      <AuthScreen description="Enter the code we sent you." footer={null}>
        <VerifyStep
          sent={sent}
          onBack={() => {
            setSent(undefined);
          }}
        />
      </AuthScreen>
    );
  }

  return (
    <AuthScreen
      description="We'll send you a six digit code. There is no password to remember."
      footer={null}
    >
      <MethodPicker method={method} onChange={setMethod} />
      {method === "email" ? <EmailRequest onSent={setSent} /> : <PhoneRequest onSent={setSent} />}
    </AuthScreen>
  );
};

export default SignIn;
