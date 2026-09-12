import { QueryError } from "@/components/app/errors.js";
import { PressableScale } from "@/components/motion/pressable-scale.js";
import { PersonAvatar, usePersonName } from "@/components/profile/avatar.js";
import { Sign, SignText } from "@/components/sign/sign.js";
import { Text } from "@/components/ui/text.js";
import { colors } from "@/lib/theme.js";
import { useAtomMount, useAtomValue } from "@effect/atom-react";
import Ionicons from "@expo/vector-icons/Ionicons";
import { chatPushesAtom, tripConversationAtom } from "@surge/common/atom/chat-atoms";
import { formatClock } from "@surge/common/chat/thread";
import type { TripId, UserId } from "@surge/domain/api/Primitives";
import { type Conversation, isOpen } from "@surge/domain/chat/Chat";
import { AsyncResult } from "effect/unstable/reactivity";
import { router } from "expo-router";
import { ActivityIndicator, View } from "react-native";
import Animated, { FadeInDown, ReduceMotion } from "react-native-reanimated";

/** Opens a conversation as the sheet that rises over whatever screen the person is on. */
export const openConversation = (conversationId: Conversation["id"]) => {
  router.push({ pathname: "/conversation/[id]", params: { id: conversationId } });
};

/**
 * The person on the other side of a trip, as a ride app puts them in front of
 * you — the web's `CounterpartCard` for the phone: their face, first name and
 * what the trip is doing, and a yellow Message pill with what is unread riding
 * on it in ink. The pill opens the conversation as a sheet over the trip.
 */
export const CounterpartCard = (props: {
  readonly tripId: TripId;
  readonly userId: UserId;
  /** Who they are until they give a name: "Your driver", "Your rider". */
  readonly relation: string;
  readonly detail: string;
}) => {
  useAtomMount(chatPushesAtom);
  const name = usePersonName(props.userId, props.relation);
  const conversation = useAtomValue(tripConversationAtom(props.tripId));

  return (
    <Animated.View
      entering={FadeInDown.duration(240).reduceMotion(ReduceMotion.System)}
      className="gap-2"
    >
      <Sign>
        <PersonAvatar userId={props.userId} size="lg" />
        <View className="min-w-0 flex-1">
          <SignText numberOfLines={1} className="text-lg leading-tight font-semibold">
            {name}
          </SignText>
          <SignText numberOfLines={1} className="text-[13px] opacity-70">
            {name === props.relation ? props.detail : `${props.relation} · ${props.detail}`}
          </SignText>
        </View>
        {AsyncResult.isInitial(conversation)
          ? (
            <View className="h-11 w-12 items-center justify-center">
              <ActivityIndicator color={colors.mutedForeground} />
            </View>
          )
          : AsyncResult.isSuccess(conversation) && isOpen(conversation.value)
          ? <MessagePill conversation={conversation.value} name={name} />
          : null}
      </Sign>
      {AsyncResult.isFailure(conversation) && (
        <QueryError result={conversation} subject={`messages with ${name}`} />
      )}
    </Animated.View>
  );
};

const MessagePill = (props: { readonly conversation: Conversation; readonly name: string; }) => {
  const { unread } = props.conversation;
  return (
    <PressableScale
      accessibilityLabel={`Message ${props.name}${
        unread > 0 ? `, ${unread} new ${unread === 1 ? "message" : "messages"}` : ""
      }`}
      onPress={() => {
        openConversation(props.conversation.id);
      }}
      className="h-11 flex-row items-center gap-2 rounded-full bg-primary pr-3 pl-4 active:bg-[#f0c400]"
    >
      <Ionicons name="chatbubble" size={16} color={colors.foreground} />
      <Text className="text-[15px] font-semibold text-primary-foreground">Message</Text>
      {unread > 0 && (
        <View className="h-6 min-w-6 items-center justify-center rounded-full bg-secondary px-1.5">
          <Text className="text-xs font-semibold text-secondary-foreground tabular-nums">
            {unread > 99 ? "99+" : unread}
          </Text>
        </View>
      )}
    </PressableScale>
  );
};

/**
 * After a trip, the driver is still in reach for as long as the conversation
 * takes messages — an hour after the trip, for the phone on the back seat.
 * Nothing once it has closed; the conversation stays in Messages either way.
 */
export const AfterTripChat = (props: {
  readonly tripId: TripId;
  readonly userId: UserId;
  readonly relation: string;
}) => {
  const conversation = useAtomValue(tripConversationAtom(props.tripId));
  if (!AsyncResult.isSuccess(conversation) || !isOpen(conversation.value)) return null;
  const { closesAt } = conversation.value;
  return (
    <CounterpartCard
      tripId={props.tripId}
      userId={props.userId}
      relation={props.relation}
      detail={closesAt === ""
        ? "Left something behind?"
        : `Left something? Until ${formatClock(closesAt)}`}
    />
  );
};
