import { sendCode, verifyCode } from "@/atom/session-atoms.js";
import { AuthScreen } from "@/components/auth/auth-screen.js";
import { OtpInput } from "@/components/auth/otp-input.js";
import { roleField } from "@/components/auth/role-field.js";
import { textField } from "@/components/auth/text-field.js";
import { Alert, AlertDescription } from "@/components/ui/alert.js";
import { Button } from "@/components/ui/button.js";
import { Text } from "@/components/ui/text.js";
import { useRestartSession } from "@/iam/session-scope.js";
import { serviceUrls } from "@/lib/config.js";
import { cn } from "@/lib/utils.js";
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
import { Pressable, View } from "react-native";
import Animated, { FadeIn, ReduceMotion } from "react-native-reanimated";

/**
 * Signing in and signing up are one screen, as on the web: an email address —
 * or, offered quietly, a phone number — then the code sent to it. The first
 * code verified for an address makes the account, with the role chosen here,
 * so nobody has to know in advance which of the two they are doing, and there
 * is no password to forget.
 */

/** Where a code went, and the role an account made by it gets. */
interface Sent {
  readonly to: Contact;
  readonly role: SignUpRole;
}

type Step =
  | { readonly kind: "email"; }
  | { readonly kind: "phone"; }
  | { readonly kind: "verify"; readonly sent: Sent; };

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

/** The code as six boxes under its label, sent the moment the last digit lands. */
const codeField = (props: {
  readonly field: {
    readonly value: string;
    readonly onChange: (value: string) => void;
    readonly onBlur: () => void;
    readonly error: Option.Option<string>;
    readonly isDirty: boolean;
  };
  readonly props: {
    readonly submitted?: boolean;
    readonly status?: "idle" | "error" | "success";
    readonly onComplete?: () => void;
  };
}) => {
  const reveal = props.field.isDirty || props.props.submitted === true;
  const error = reveal ? props.field.error : Option.none();

  return (
    <View className="gap-2">
      <Text className="text-[13px] font-semibold text-muted-foreground">Six-digit code</Text>
      <OtpInput
        testID="field-code"
        accessibilityLabel="Six-digit code"
        accessibilityHint={Option.getOrUndefined(error)}
        value={props.field.value}
        onChange={props.field.onChange}
        onBlur={props.field.onBlur}
        onComplete={props.props.onComplete}
        autoFocus
        status={Option.isSome(error) ? "error" : props.props.status ?? "idle"}
      />
      {Option.isSome(error) && (
        <Animated.View entering={FadeIn.duration(160).reduceMotion(ReduceMotion.System)}>
          <Text className="text-[13px] font-medium text-destructive">{error.value}</Text>
        </Animated.View>
      )}
    </View>
  );
};

const codeForm = FormReact.make(
  FormBuilder.empty.addField("code", OtpCode),
  {
    mode: { validation: "onBlur" },
    fields: { code: codeField },
    onSubmit: (sent: Sent, { decoded }) =>
      verifyCode({ to: sent.to, code: decoded.code, role: sent.role }),
  },
);

/** A new code for the same address, when the first did not arrive or ran out. */
const resendAtom = Atom.fn((to: Contact) => sendCode(to));

/** The code the auth service's dev outbox holds, for a development build to show. */
const devCode = devCodeFamily(serviceUrls.AUTH_BASE_URL);

const Refused = (props: { readonly message: string; }) => (
  <Alert variant="destructive">
    <AlertDescription className="text-destructive">{props.message}</AlertDescription>
  </Alert>
);

/**
 * A quiet way to somewhere else on this sheet — the web's `Aside`: small,
 * grey, centred under the action with air above it, so it reads as an aside
 * rather than a second choice.
 */
const Aside = (props: { readonly children: React.ReactNode; }) => (
  <View className="flex-row flex-wrap items-center justify-center">{props.children}</View>
);

const AsideText = (props: { readonly children: React.ReactNode; }) => (
  <Text className="text-[13px] leading-5 text-muted-foreground/80">{props.children}</Text>
);

/** The link in an aside: it inks in under the thumb, with room around it to be hit. */
const AsideLink = (props: {
  readonly testID?: string;
  readonly disabled?: boolean;
  readonly onPress: () => void;
  readonly children: string;
}) => (
  <Pressable
    testID={props.testID}
    accessibilityRole="button"
    accessibilityState={{ disabled: props.disabled === true }}
    disabled={props.disabled}
    hitSlop={{ top: 12, bottom: 12, left: 6, right: 6 }}
    onPress={props.onPress}
  >
    {({ pressed }) => (
      <Text
        className={cn(
          "text-[13px] leading-5 font-medium",
          pressed ? "text-foreground" : "text-muted-foreground",
          props.disabled === true && "opacity-50",
        )}
      >
        {props.children}
      </Text>
    )}
  </Pressable>
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
      <View className="gap-5">
        <emailForm.email submitted={submitted} />
        <emailForm.role submitted={submitted} />
        {result._tag === "Failure" && (
          <Refused
            message={submitMessage(result, "We could not send a code there. Try again shortly.")}
          />
        )}
        <Button
          disabled={result.waiting}
          onPress={() => {
            submit();
          }}
        >
          {result.waiting ? "Sending…" : "Email me a code"}
        </Button>
        <View className="pt-1">
          <Aside>
            <AsideText>No email to hand?{" "}</AsideText>
            <AsideLink testID="method-phone" onPress={props.onPhone}>
              Text it to your phone
            </AsideLink>
          </Aside>
        </View>
      </View>
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
      <View className="gap-5">
        <phoneForm.phoneNumber submitted={submitted} />
        <phoneForm.role submitted={submitted} />
        {result._tag === "Failure" && (
          <Refused
            message={submitMessage(result, "We could not text a code there. Try again shortly.")}
          />
        )}
        <Button
          disabled={result.waiting}
          onPress={() => {
            submit();
          }}
        >
          {result.waiting ? "Sending…" : "Text me a code"}
        </Button>
        <View className="pt-1">
          <Aside>
            <AsideLink testID="method-email" onPress={props.onEmail}>
              Use your email instead
            </AsideLink>
          </Aside>
        </View>
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
    <View className="rounded-2xl bg-tile px-4 py-3">
      <Text className="text-sm text-muted-foreground">
        Development build: the code is{" "}
        <Text className="text-sm font-semibold tracking-widest text-foreground tabular-nums">
          {code.value.value}
        </Text>
      </Text>
    </View>
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
  const restart = useRestartSession();
  const byEmail = props.sent.to.kind === "email";

  /**
   * The cookie is in SecureStore once this succeeds. Restarting the scope
   * reads the session afresh, and the navigator swaps to the app on its own.
   */
  React.useEffect(() => {
    if (result._tag === "Success") restart();
  }, [result, restart]);

  const status = result._tag === "Success"
    ? "success"
    : result._tag === "Failure" && !result.waiting
    ? "error"
    : "idle";

  return (
    <codeForm.Initialize defaultValues={{ code: "" }}>
      <View className="gap-5">
        <Text className="text-[15px] leading-[22px] text-muted-foreground">
          We sent it to{" "}
          <Text className="text-[15px] font-semibold text-foreground">
            {describeContact(props.sent.to)}
          </Text>.
        </Text>
        {__DEV__ && <DevCode address={address} />}
        <codeForm.code
          submitted={submitted}
          status={status}
          onComplete={() => {
            if (!result.waiting) submit(props.sent);
          }}
        />
        {result._tag === "Failure" && (
          <Refused
            message={submitMessage(
              result,
              "That code is wrong or has expired. Send a new one if it has.",
            )}
          />
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
          <Text
            accessibilityLiveRegion="polite"
            className="text-center text-sm text-muted-foreground"
          >
            A new code is on its way.
          </Text>
        )}
        {resending._tag === "Failure" && (
          <Refused
            message={submitMessage(resending, "We could not send a new code. Try again shortly.")}
          />
        )}
        <View className="gap-2 pt-3">
          <Aside>
            <AsideText>Nothing yet?{" "}</AsideText>
            <AsideLink
              disabled={resending.waiting}
              onPress={() => {
                // A refused resend is shown from its own result, above.
                void resend(props.sent.to).then(refreshDevCode, () => {});
              }}
            >
              Send a new code
            </AsideLink>
          </Aside>
          <Aside>
            <AsideLink
              onPress={() => {
                props.onSwitch(byEmail ? "phone" : "email");
              }}
            >
              {byEmail ? "Or text it to your phone" : "Or email it instead"}
            </AsideLink>
            <AsideText>{" · "}</AsideText>
            <AsideLink
              onPress={() => {
                props.onSwitch(byEmail ? "email" : "phone");
              }}
            >
              {byEmail ? "Use a different email" : "Use a different number"}
            </AsideLink>
          </Aside>
        </View>
      </View>
    </codeForm.Initialize>
  );
};

const SignIn = () => {
  const [step, setStep] = React.useState<Step>({ kind: "email" });
  const verifying = step.kind === "verify";
  const onSent = (sent: Sent) => {
    setStep({ kind: "verify", sent });
  };

  return (
    <AuthScreen
      title={verifying ? "Enter your code" : "Welcome to Surge"}
      description={verifying
        ? "It expires in five minutes."
        : "Sign in or create an account with a six-digit code. There is no password to remember."}
      step={step.kind}
      direction={step.kind === "email" ? -1 : 1}
    >
      {step.kind === "email" && (
        <EmailRequest
          onSent={onSent}
          onPhone={() => {
            setStep({ kind: "phone" });
          }}
        />
      )}
      {step.kind === "phone" && (
        <PhoneRequest
          onSent={onSent}
          onEmail={() => {
            setStep({ kind: "email" });
          }}
        />
      )}
      {step.kind === "verify" && (
        <VerifyStep
          sent={step.sent}
          onSwitch={(kind) => {
            setStep({ kind });
          }}
        />
      )}
    </AuthScreen>
  );
};

export default SignIn;
