import { sessionAtom, signOut } from "@/atom/session-atoms.js";
import { UnreadCount, unreadWords, useUnreadTotal } from "@/chat/unread-count.js";
import { ActionSheet } from "@/components/app/action-sheet.js";
import { Detail, Details } from "@/components/app/details.js";
import { ActionError } from "@/components/app/errors.js";
import { Screen } from "@/components/app/screen.js";
import { PressableScale } from "@/components/motion/pressable-scale.js";
import { Avatar } from "@/components/profile/avatar.js";
import { IconBubble, Sign, SignButton, SignText } from "@/components/sign/sign.js";
import { Button } from "@/components/ui/button.js";
import { Text } from "@/components/ui/text.js";
import { useRestartSession } from "@/iam/session-scope.js";
import { serviceUrls } from "@/lib/config.js";
import { colors } from "@/lib/theme.js";
import { PaymentRow } from "@/ride/card-summary.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import Ionicons from "@expo/vector-icons/Ionicons";
import { myProfileAtom } from "@surge/common/atom/profile-atoms";
import { contactOf, describeContact } from "@surge/domain/iam/Contact";
import type { Role } from "@surge/domain/iam/Identity";
import { Exit } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";
import { router } from "expo-router";
import * as React from "react";
import { View } from "react-native";

/**
 * Who is signed in, the way a ride app's account opens: the name and face
 * others see, large, with the way to change them; a row of shortcuts; the
 * card; and, quietly at the foot, which services this phone is talking to —
 * on a phone the usual reason nothing works is that one of them is not
 * reachable from the device.
 */
const Account = () => {
  const session = useAtomValue(sessionAtom);
  if (!AsyncResult.isSuccess(session)) return null;
  const identity = session.value;
  const people = identity.role === "rider" || identity.role === "driver";

  return (
    <Screen title="Account">
      <ProfileHeader
        contact={describeContact(contactOf(identity.email))}
        role={identity.role}
      />

      {people && (
        <View className="flex-row gap-2">
          <MessagesTile />
          <Tile
            icon="help-buoy-outline"
            label="Help"
            onPress={() => {
              router.push("/support/new");
            }}
          />
          {identity.role === "driver"
            ? (
              <Tile
                icon="wallet-outline"
                label="Earnings"
                onPress={() => {
                  router.navigate("/earnings");
                }}
              />
            )
            : (
              <Tile
                icon="car-outline"
                label="Ride"
                onPress={() => {
                  router.navigate("/ride");
                }}
              />
            )}
        </View>
      )}

      {identity.role === "ops" && (
        <Sign>
          <IconBubble name="desktop-outline" />
          <SignText className="flex-1 leading-5">
            The dispatch console and the support desk are on the web. This app is for riders and
            drivers.
          </SignText>
        </Sign>
      )}

      {identity.role === "rider" && (
        <View className="gap-2">
          <Text accessibilityRole="header" className="px-1 text-base font-semibold">Payment</Text>
          <PaymentRow />
        </View>
      )}

      <View className="gap-3 rounded-2xl bg-tile p-4">
        <Text
          accessibilityRole="header"
          className="text-[13px] font-semibold text-muted-foreground"
        >
          This phone talks to
        </Text>
        <Details>
          <Detail label="API" value={serviceUrls.SURGE_API_URL} mono />
          <Detail label="Socket" value={serviceUrls.SURGE_WS_URL} mono />
          <Detail label="Auth" value={serviceUrls.AUTH_BASE_URL} mono />
        </Details>
      </View>

      <SignOut />
    </Screen>
  );
};

/**
 * The name and face others see, large, and the way to change them. While
 * either is missing a yellow card asks for it — it is the next step.
 */
const ProfileHeader = (props: { readonly contact: string; readonly role: Role; }) => {
  const profile = useAtomValue(myProfileAtom);
  const value = AsyncResult.isSuccess(profile) ? profile.value : undefined;
  const name = value?.displayName ?? "";
  const incomplete = value !== undefined && (value.displayName === "" || value.avatarUrl === "");
  const edit = () => {
    router.push("/profile");
  };

  return (
    <View className="gap-4">
      <PressableScale
        testID="edit-profile"
        scaleTo={0.98}
        accessibilityLabel={`${name === "" ? "Add your name" : name}. Edit your profile`}
        onPress={edit}
        className="flex-row items-center gap-4"
      >
        <View className="min-w-0 flex-1 gap-0.5">
          <Text numberOfLines={1} className="text-2xl leading-[30px] font-semibold">
            {name === "" ? "Add your name" : name}
          </Text>
          <Text numberOfLines={1} className="text-[15px] text-muted-foreground">
            {props.contact}
          </Text>
          <View className="mt-1.5 flex-row items-center gap-0.5">
            <Text className="text-[13px] font-semibold">Edit profile</Text>
            <Ionicons name="chevron-forward" size={14} color={colors.foreground} />
          </View>
        </View>
        <Avatar src={value?.avatarUrl} name={name} size="hero" />
      </PressableScale>

      {incomplete && props.role !== "ops" && (
        <SignButton tone="direction" onPress={edit}>
          <IconBubble name="camera-outline" />
          <View className="min-w-0 flex-1">
            <SignText className="text-base font-semibold">
              {props.role === "driver" ? "Riders look for your face" : "Help your driver find you"}
            </SignText>
            <SignText className="text-[13px] opacity-80">Add your first name and a photo.</SignText>
          </View>
          <Ionicons name="chevron-forward" size={18} color={colors.foreground} />
        </SignButton>
      )}
    </View>
  );
};

const Tile = (props: {
  readonly icon: React.ComponentProps<typeof Ionicons>["name"];
  readonly label: string;
  readonly onPress: () => void;
  readonly count?: number;
}) => {
  const count = props.count ?? 0;
  return (
    <PressableScale
      scaleTo={0.95}
      accessibilityLabel={count > 0 ? `${props.label}, ${unreadWords(count)}` : props.label}
      onPress={props.onPress}
      className="flex-1 gap-3 rounded-2xl bg-tile p-4 active:bg-tile-hover"
    >
      <View className="h-6 flex-row items-start justify-between">
        <Ionicons name={props.icon} size={24} color={colors.foreground} />
        {count > 0 && <UnreadCount count={count} />}
      </View>
      <Text className="text-[15px] font-semibold">{props.label}</Text>
    </PressableScale>
  );
};

const MessagesTile = () => {
  const unread = useUnreadTotal();
  return (
    <Tile
      icon="chatbubbles-outline"
      label="Messages"
      count={unread}
      onPress={() => {
        router.navigate("/messages");
      }}
    />
  );
};

/** Signing out, asked once first: it is a long way back in with a code. */
const SignOut = () => {
  const leaving = useAtomValue(signOut);
  const leave = useAtomSet(signOut, { mode: "promiseExit" });
  const restart = useRestartSession();
  const [asking, setAsking] = React.useState(false);

  return (
    <View className="gap-2">
      {AsyncResult.isFailure(leaving) && <ActionError cause={leaving.cause} />}
      <Button
        testID="sign-out"
        variant="destructive"
        disabled={leaving.waiting}
        onPress={() => {
          setAsking(true);
        }}
      >
        {leaving.waiting ? "Signing out…" : "Sign out"}
      </Button>
      <ActionSheet
        visible={asking}
        title="Sign out of Surge?"
        message="You will need a new code to sign back in."
        onClose={() => {
          setAsking(false);
        }}
        options={[{
          label: "Sign out",
          icon: "log-out-outline",
          danger: true,
          testID: "confirm-sign-out",
          onPress: () => {
            void leave().then((exit) => {
              if (Exit.isSuccess(exit)) restart();
            });
          },
        }]}
      />
    </View>
  );
};

export default Account;
