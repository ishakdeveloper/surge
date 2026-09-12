import { sessionAtom } from "@/atom/session-atoms.js";
import { openConversation } from "@/chat/counterpart-card.js";
import { UnreadCount, unreadWords } from "@/chat/unread-count.js";
import { QueryError } from "@/components/app/errors.js";
import { Screen } from "@/components/app/screen.js";
import { PressableScale } from "@/components/motion/pressable-scale.js";
import { PersonAvatar, usePersonName } from "@/components/profile/avatar.js";
import { Button } from "@/components/ui/button.js";
import { Text } from "@/components/ui/text.js";
import { colors } from "@/lib/theme.js";
import { useAtomMount, useAtomValue } from "@effect/atom-react";
import Ionicons from "@expo/vector-icons/Ionicons";
import { chatPushesAtom, conversationsAtom } from "@surge/common/atom/chat-atoms";
import { conversationTitle, roleName, statusLine, whenLabel } from "@surge/common/chat/thread";
import type { UserId } from "@surge/domain/api/Primitives";
import type { Conversation, Participant } from "@surge/domain/chat/Chat";
import { AsyncResult } from "effect/unstable/reactivity";
import { router } from "expo-router";
import * as React from "react";
import { View } from "react-native";
import Animated, { FadeInDown, ReduceMotion } from "react-native-reanimated";

/**
 * Everything a rider or driver has said and been told — the web's Messages
 * page for the phone: the chat with the other side of each recent trip, and
 * their conversations with support, newest first, what is unread counted in
 * yellow. Asking support for help starts here too.
 */
const Messages = () => {
  useAtomMount(chatPushesAtom);
  const conversations = useAtomValue(conversationsAtom);
  const me = useAtomValue(
    sessionAtom,
    (session) => AsyncResult.isSuccess(session) ? session.value.userId : undefined,
  );
  // oxlint-disable-next-line effecttsgo/global-date
  const [now] = React.useState(() => Date.now());

  return (
    <Screen title="Messages">
      {AsyncResult.isInitial(conversations)
        ? <Text className="text-muted-foreground">Loading your messages…</Text>
        : AsyncResult.isFailure(conversations)
        ? <QueryError result={conversations} subject="your messages" />
        : conversations.value.length === 0 || me === undefined
        ? (
          <View className="items-center gap-3 rounded-3xl bg-tile px-6 py-8">
            <View className="size-14 items-center justify-center rounded-full bg-card">
              <Ionicons name="chatbubbles-outline" size={26} color={colors.foreground} />
            </View>
            <Text className="text-[17px] font-semibold">No messages yet</Text>
            <Text className="text-center text-[15px] leading-5 text-muted-foreground">
              When a driver accepts a trip, you can message each other from the trip, and the
              conversation shows up here.
            </Text>
          </View>
        )
        : (
          <View className="gap-2">
            {conversations.value.map((conversation, index) => (
              <Animated.View
                key={conversation.id}
                entering={FadeInDown.delay(Math.min(index, 8) * 40).duration(220).reduceMotion(
                  ReduceMotion.System,
                )}
              >
                <ConversationRow conversation={conversation} me={me} now={now} />
              </Animated.View>
            ))}
          </View>
        )}

      <View className="gap-3 rounded-3xl bg-card p-5">
        <View className="size-11 items-center justify-center rounded-full bg-tile">
          <Ionicons name="help-buoy-outline" size={22} color={colors.foreground} />
        </View>
        <View className="gap-1">
          <Text accessibilityRole="header" className="text-base font-semibold">Need a hand?</Text>
          <Text className="text-[15px] leading-5 text-muted-foreground">
            Something wrong with a trip, a fare or your account? Surge support replies here.
          </Text>
        </View>
        <Button
          variant="outline"
          onPress={() => {
            router.push("/support/new");
          }}
        >
          Contact support
        </Button>
      </View>
    </Screen>
  );
};

/** One conversation: a trip's with the other side's face and name, support's with the lifebuoy. */
const ConversationRow = (props: {
  readonly conversation: Conversation;
  readonly me: UserId;
  readonly now: number;
}) => {
  const other = props.conversation.kind === "CONVERSATION_KIND_TRIP"
    ? props.conversation.participants.find((participant) => participant.userId !== props.me)
    : undefined;
  return other === undefined
    ? (
      <Row
        {...props}
        name={conversationTitle(props.conversation, props.me, false)}
        face={
          <View className="size-11 items-center justify-center rounded-full bg-card">
            <Ionicons name="help-buoy" size={22} color={colors.foreground} />
          </View>
        }
      />
    )
    : <TripRow {...props} other={other} />;
};

const TripRow = (props: {
  readonly conversation: Conversation;
  readonly me: UserId;
  readonly now: number;
  readonly other: Participant;
}) => {
  const name = usePersonName(props.other.userId, roleName(props.other.role, false));
  return <Row {...props} name={name} face={<PersonAvatar userId={props.other.userId} />} />;
};

const Row = (props: {
  readonly conversation: Conversation;
  readonly me: UserId;
  readonly now: number;
  readonly name: string;
  readonly face: React.ReactNode;
}) => {
  const { conversation } = props;
  const status = statusLine(conversation, props.me, false);
  const unread = conversation.unread;
  return (
    <PressableScale
      scaleTo={0.98}
      accessibilityLabel={`${props.name}. ${unread > 0 ? `${unreadWords(unread)}. ` : ""}${status}`}
      onPress={() => {
        openConversation(conversation.id);
      }}
      className="flex-row items-center gap-3 rounded-2xl bg-tile px-4 py-3.5 active:bg-tile-hover"
    >
      {props.face}
      <View className="min-w-0 flex-1 gap-0.5">
        <View className="flex-row items-baseline gap-2">
          <Text numberOfLines={1} className="flex-1 text-base font-semibold">{props.name}</Text>
          <Text className="text-xs text-muted-foreground tabular-nums">
            {whenLabel(conversation.updatedAt, props.now)}
          </Text>
        </View>
        <Text
          numberOfLines={1}
          className={unread > 0
            ? "text-[13px] font-semibold text-foreground"
            : "text-[13px] text-muted-foreground"}
        >
          {status}
        </Text>
      </View>
      {unread > 0 && <UnreadCount count={unread} />}
    </PressableScale>
  );
};

export default Messages;
