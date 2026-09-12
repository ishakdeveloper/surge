import { sessionAtom } from "@/atom/session-atoms.js";
import { ActionSheet } from "@/components/app/action-sheet.js";
import { ActionError, QueryError } from "@/components/app/errors.js";
import { SheetHeader } from "@/components/app/sheet-header.js";
import { PressableScale } from "@/components/motion/pressable-scale.js";
import { Button } from "@/components/ui/button.js";
import { Input } from "@/components/ui/input.js";
import { Text } from "@/components/ui/text.js";
import { cn } from "@/lib/utils.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { FormBuilder, FormReact } from "@lucas-barake/effect-form-react";
import { SurgeApi } from "@surge/client/SurgeApi";
import { Keys } from "@surge/common/atom/reactivity-keys";
import { runtime } from "@surge/common/atom/runtime";
import { tripsAtom } from "@surge/common/atom/trip-atoms";
import { SupportBody, SupportSubject } from "@surge/common/chat/support";
import { driverStatus, formatCents, riderStatus } from "@surge/common/lib/format";
import { TripId } from "@surge/domain/api/Primitives";
import { Cause, Effect, Option, Schema } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";
import { randomUUID } from "expo-crypto";
import { router, Stack } from "expo-router";
import * as React from "react";
import { ScrollView, View } from "react-native";
import Animated, {
  FadeIn,
  ReduceMotion,
  useAnimatedKeyboard,
  useAnimatedStyle,
} from "react-native-reanimated";
import { useSafeAreaInsets } from "react-native-safe-area-context";

/**
 * Asking Surge support for help, as a sheet — the web's contact form for the
 * phone: what it is about, which trip if any, and what happened. Sending
 * opens the conversation in its place, where support replies.
 */

/** What effect-form hands a field renderer. */
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

/** Hidden until the field has been typed in or a submit was tried. */
const shownError = (props: FieldProps) =>
  props.field.isDirty || props.props.submitted === true ? props.field.error : Option.none();

const Label = (props: { readonly children: React.ReactNode; }) => (
  <Text className="text-[13px] font-semibold text-muted-foreground">{props.children}</Text>
);

const FieldError = (props: { readonly message: string; }) => (
  <Animated.View entering={FadeIn.duration(160).reduceMotion(ReduceMotion.System)}>
    <Text accessibilityRole="alert" className="text-[13px] font-medium text-destructive">
      {props.message}
    </Text>
  </Animated.View>
);

const SubjectField = (props: FieldProps) => {
  const error = shownError(props);
  return (
    <View className="gap-2">
      <Label>What is it about?</Label>
      <Input
        testID="field-subject"
        accessibilityLabel="What is it about?"
        accessibilityHint={Option.getOrUndefined(error)}
        value={props.field.value}
        onChangeText={props.field.onChange}
        onBlur={props.field.onBlur}
        maxLength={120}
        autoCapitalize="sentences"
        returnKeyType="next"
        placeholder="A lost item, a fare, your account…"
        invalid={Option.isSome(error)}
      />
      {Option.isSome(error) && <FieldError message={error.value} />}
    </View>
  );
};

/** Which recent trip it is about, if any, as pills to tap. */
const TripField = (props: FieldProps) => {
  const trips = useAtomValue(tripsAtom);
  const role = useAtomValue(
    sessionAtom,
    (session) => AsyncResult.isSuccess(session) ? session.value.role : undefined,
  );
  const status = role === "driver" ? driverStatus : riderStatus;
  const choices = [
    { id: "", label: "Not about a trip" },
    ...(AsyncResult.isSuccess(trips)
      ? trips.value.slice(0, 6).map((trip) => ({
        id: trip.id as string,
        label: `${status[trip.status]} · ${formatCents(trip.totalCents)}`,
      }))
      : []),
  ];

  return (
    <View className="gap-2">
      <Label>About a trip (optional)</Label>
      <ScrollView
        horizontal
        showsHorizontalScrollIndicator={false}
        keyboardShouldPersistTaps="handled"
        accessibilityRole="radiogroup"
        accessibilityLabel="About a trip"
        className="-mx-5"
        contentContainerClassName="gap-2 px-5"
      >
        {choices.map((choice) => {
          const chosen = props.field.value === choice.id;
          return (
            <PressableScale
              key={choice.id === "" ? "none" : choice.id}
              accessibilityRole="radio"
              accessibilityState={{ checked: chosen }}
              feedback="select"
              onPress={() => {
                props.field.onChange(choice.id);
                props.field.onBlur();
              }}
              className={cn(
                "h-10 justify-center rounded-full px-4",
                chosen ? "bg-primary" : "bg-tile active:bg-tile-hover",
              )}
            >
              <Text className="text-sm font-semibold">{choice.label}</Text>
            </PressableScale>
          );
        })}
      </ScrollView>
      {AsyncResult.isFailure(trips) && <QueryError result={trips} subject="your trips" />}
    </View>
  );
};

const BodyField = (props: FieldProps) => {
  const error = shownError(props);
  return (
    <View className="gap-2">
      <Label>What happened?</Label>
      <Input
        testID="field-body"
        accessibilityLabel="What happened?"
        accessibilityHint={Option.getOrUndefined(error)}
        value={props.field.value}
        onChangeText={props.field.onChange}
        onBlur={props.field.onBlur}
        multiline
        maxLength={1000}
        textAlignVertical="top"
        autoCapitalize="sentences"
        invalid={Option.isSome(error)}
        className="h-36 pt-3 pb-3 leading-5"
      />
      {Option.isSome(error)
        ? <FieldError message={error.value} />
        : (
          <Text className="text-[13px] text-muted-foreground">
            When and where it happened helps support find it quickly.
          </Text>
        )}
    </View>
  );
};

/**
 * The request, sent the way `contactSupport` sends it. The form owns the
 * call, so its submit state — waiting, refused, done — is one thing to read.
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
    fields: { subject: SubjectField, tripId: TripField, body: BodyField },
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

const ContactSupport = () => {
  const submit = useAtomSet(supportForm.submit);
  const result = useAtomValue(supportForm.submit);
  const submitted = useAtomValue(supportForm.submitCount) > 0;
  const dirty = useAtomValue(supportForm.isDirty);
  // One key for this request, kept across retries: sending again after a
  // failure is the same request, and support sees one conversation.
  const [key] = React.useState(() => randomUUID());
  const [leaving, setLeaving] = React.useState(false);
  const sent = AsyncResult.isSuccess(result);
  const unsent = dirty && !sent;

  const keyboard = useAnimatedKeyboard();
  const insets = useSafeAreaInsets();
  const lifted = useAnimatedStyle(() => ({
    paddingBottom: Math.max(keyboard.height.value, insets.bottom) + 8,
  }));

  React.useEffect(() => {
    if (!AsyncResult.isSuccess(result)) return;
    router.replace({ pathname: "/conversation/[id]", params: { id: result.value.id } });
  }, [result]);

  return (
    <View className="flex-1 bg-card">
      {/* What was typed and not sent is the one thing a swipe would lose. */}
      <Stack.Screen options={{ gestureEnabled: !unsent }} />
      <SheetHeader
        title="Contact support"
        subtitle="Surge support replies in Messages."
        onClose={() => {
          if (unsent) setLeaving(true);
          else router.back();
        }}
      />
      <supportForm.Initialize defaultValues={{ subject: "", tripId: "", body: "" }}>
        <ScrollView
          className="flex-1"
          contentContainerClassName="gap-5 px-5 pt-3 pb-6"
          keyboardShouldPersistTaps="handled"
          keyboardDismissMode="interactive"
        >
          <supportForm.subject submitted={submitted} />
          <supportForm.tripId submitted={submitted} />
          <supportForm.body submitted={submitted} />
          {AsyncResult.isFailure(result) && !result.waiting && !isValidation(result.cause) && (
            <ActionError cause={result.cause} />
          )}
        </ScrollView>
        <Animated.View className="px-5 pt-2" style={lifted}>
          <Button
            disabled={result.waiting}
            onPress={() => {
              submit(key);
            }}
          >
            {result.waiting ? "Sending…" : "Send to support"}
          </Button>
        </Animated.View>
      </supportForm.Initialize>

      <ActionSheet
        visible={leaving}
        title="Leave without sending?"
        message="What you wrote has not been sent to support, and leaving discards it."
        onClose={() => {
          setLeaving(false);
        }}
        options={[{
          label: "Discard and leave",
          icon: "trash-outline",
          danger: true,
          onPress: () => {
            router.back();
          },
        }]}
      />
    </View>
  );
};

export default ContactSupport;
