import { PressableScale } from "@/components/motion/pressable-scale.js";
import { Text } from "@/components/ui/text.js";
import { colors } from "@/lib/theme.js";
import { cn } from "@/lib/utils.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import Ionicons from "@expo/vector-icons/Ionicons";
import { announceTyping, conversationAtom, sendMessage } from "@surge/common/atom/chat-atoms";
import { draftAtom } from "@surge/common/atom/chat-drafts";
import { closedNote, counterpartName } from "@surge/common/chat/thread";
import type { ConversationId, UserId } from "@surge/domain/api/Primitives";
import { type Conversation, isOpen } from "@surge/domain/chat/Chat";
import { AsyncResult } from "effect/unstable/reactivity";
import { randomUUID } from "expo-crypto";
import * as React from "react";
import { ScrollView, TextInput, View } from "react-native";
import Animated, { FadeIn, FadeOut, ReduceMotion } from "react-native-reanimated";

/** The server's limit on a message's text. */
const MAX_LENGTH = 1000;

/**
 * Where a message is written — the web's `ChatComposer` for the phone: the
 * conversation's quick replies as one-tap pills while nothing is typed, a grey
 * field that grows with what is written, and a round yellow send — the next
 * step — that stays grey until there is something to send.
 *
 * What is typed is kept per conversation for the session, so leaving and
 * coming back finds it. A closed conversation shows why instead.
 */
export const ChatComposer = (props: {
  readonly conversationId: ConversationId;
  readonly staff?: boolean;
  readonly autoFocus?: boolean;
}) => {
  const view = useAtomValue(conversationAtom(props.conversationId));
  if (!AsyncResult.isSuccess(view)) return null;
  const { conversation, me } = view.value;
  const staff = props.staff === true;

  if (!isOpen(conversation)) {
    return (
      <View className="px-3 pt-2">
        <Text className="rounded-2xl bg-tile px-4 py-3 text-sm leading-5 text-muted-foreground">
          {closedNote(conversation, staff)}
        </Text>
      </View>
    );
  }
  return (
    <Composer
      conversation={conversation}
      me={me}
      staff={staff}
      autoFocus={props.autoFocus === true}
    />
  );
};

const Composer = (props: {
  readonly conversation: Conversation;
  readonly me: UserId;
  readonly staff: boolean;
  readonly autoFocus: boolean;
}) => {
  const { conversation } = props;
  const draft = useAtomValue(draftAtom(conversation.id));
  const setDraft = useAtomSet(draftAtom(conversation.id));
  const send = useAtomSet(sendMessage);
  const typing = useAtomSet(announceTyping);
  const [focused, setFocused] = React.useState(false);
  const text = draft.text;
  const empty = text.trim() === "";

  const sendText = () => {
    const body = text.trim();
    if (body === "") return;
    send({
      conversationId: conversation.id,
      draft: { body, quickReply: "", clientMessageId: randomUUID() },
    });
    setDraft({ text: "" });
  };

  return (
    <View className="gap-2 pt-2">
      {empty && conversation.quickReplies.length > 0 && (
        <Animated.View
          entering={FadeIn.duration(160).reduceMotion(ReduceMotion.System)}
          exiting={FadeOut.duration(120).reduceMotion(ReduceMotion.System)}
        >
          <ScrollView
            horizontal
            showsHorizontalScrollIndicator={false}
            keyboardShouldPersistTaps="handled"
            accessibilityLabel="Quick replies"
            contentContainerClassName="gap-2 px-3"
          >
            {conversation.quickReplies.map((reply) => (
              <PressableScale
                key={reply.code}
                onPress={() => {
                  send({
                    conversationId: conversation.id,
                    draft: { body: "", quickReply: reply.code, clientMessageId: randomUUID() },
                  });
                }}
                className="rounded-full bg-tile px-3.5 py-2 active:bg-tile-hover"
              >
                <Text className="text-sm font-semibold">{reply.text}</Text>
              </PressableScale>
            ))}
          </ScrollView>
        </Animated.View>
      )}
      <View className="flex-row items-end gap-2 px-3">
        <TextInput
          accessibilityLabel={`Message ${
            counterpartName(conversation, props.me, props.staff).toLowerCase()
          }`}
          autoFocus={props.autoFocus}
          multiline
          maxLength={MAX_LENGTH}
          value={text}
          placeholder="Message"
          placeholderTextColor={colors.mutedForeground}
          selectionColor={colors.foreground}
          onFocus={() => {
            setFocused(true);
          }}
          onBlur={() => {
            setFocused(false);
          }}
          onChangeText={(value) => {
            setDraft({ text: value });
            if (value !== "") typing(conversation.id);
          }}
          className={cn(
            "max-h-32 min-h-11 flex-1 rounded-[22px] border-2 px-4 pt-2.5 pb-2.5 text-[15px] leading-[20px] text-foreground",
            focused ? "border-foreground bg-card" : "border-transparent bg-tile",
          )}
        />
        <PressableScale
          accessibilityLabel="Send"
          accessibilityState={{ disabled: empty }}
          disabled={empty}
          scaleTo={0.9}
          feedback="select"
          onPress={sendText}
          className={cn(
            "size-11 items-center justify-center rounded-full",
            empty ? "bg-tile" : "bg-primary",
          )}
        >
          <Ionicons
            name="arrow-up"
            size={22}
            color={empty ? colors.mutedForeground : colors.foreground}
          />
        </PressableScale>
      </View>
      {text.length >= MAX_LENGTH - 100 && (
        <Text className="px-4 text-right text-xs text-muted-foreground tabular-nums">
          {MAX_LENGTH - text.length} characters left
        </Text>
      )}
    </View>
  );
};
