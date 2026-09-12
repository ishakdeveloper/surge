import { QueryError } from "@/components/app/errors.js";
import { PersonAvatar, usePersonName } from "@/components/profile/avatar.js";
import { Text } from "@/components/ui/text.js";
import { cn } from "@/lib/utils.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import type { ConversationView, PendingMessage, SendError } from "@surge/client/Chat";
import {
  conversationAtom,
  EarlierKey,
  earlierMessagesAtom,
  markRead,
  sendMessage,
  typingAtom,
} from "@surge/common/atom/chat-atoms";
import {
  receiptLabel,
  roleName,
  type ThreadItem,
  threadItems,
  whenLabel,
} from "@surge/common/chat/thread";
import type { ConversationId, UserId } from "@surge/domain/api/Primitives";
import type { Conversation, Message } from "@surge/domain/chat/Chat";
import { Option } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";
import * as React from "react";
import { AppState, Pressable, ScrollView, View } from "react-native";
import Animated, {
  FadeInDown,
  ReduceMotion,
  useAnimatedStyle,
  useSharedValue,
  withDelay,
  withRepeat,
  withSequence,
  withTiming,
  ZoomIn,
} from "react-native-reanimated";

type Run = Extract<ThreadItem, { readonly _tag: "Run"; }>;

const theirsIn = ZoomIn.springify().damping(16).stiffness(320).reduceMotion(ReduceMotion.System);
const mineIn = FadeInDown.duration(200).reduceMotion(ReduceMotion.System);

/**
 * A conversation's messages, live — the web's `ChatThread` for the phone.
 * Runs of bubbles by sender, yours in ink on the right and theirs on grey on
 * the left with their face at the foot of the run; a time wherever the
 * conversation paused; your newest marked Sent or Seen; a send shown the
 * moment it is made, and a failed one offered again with the same key.
 *
 * It scrolls itself, and keeps the newest in view as the conversation grows
 * while the reader is at the bottom. Reading it with the app in front marks it
 * read.
 */
export const ChatThread = (
  props: { readonly conversationId: ConversationId; readonly staff?: boolean; },
) => {
  const view = useAtomValue(conversationAtom(props.conversationId));
  if (AsyncResult.isInitial(view)) {
    return (
      <View className="flex-1 items-center justify-center">
        <Text className="text-muted-foreground">Opening the conversation…</Text>
      </View>
    );
  }
  if (AsyncResult.isFailure(view)) {
    return (
      <View className="flex-1 p-4">
        <QueryError result={view} subject="this conversation" />
      </View>
    );
  }
  return <Thread view={view.value} staff={props.staff === true} />;
};

const Thread = (props: { readonly view: ConversationView; readonly staff: boolean; }) => {
  const { conversation, messages, pending, me, hasEarlier } = props.view;
  const typing = useAtomValue(typingAtom(conversation.id));
  const othersTyping = AsyncResult.isSuccess(typing)
    && typing.value.some((userId) => userId !== me);
  // oxlint-disable-next-line effecttsgo/global-date
  const [now] = React.useState(() => Date.now());
  const [openedAt] = React.useState(() => messages.at(-1)?.seq ?? 0);
  const named = props.staff || conversation.kind === "CONVERSATION_KIND_SUPPORT";
  const items = React.useMemo(() => threadItems(messages, me), [messages, me]);
  const last = messages.at(-1);
  const scroll = React.useRef<ScrollView>(null);
  const atBottom = React.useRef(true);

  useMarkRead(conversation, messages, me);

  const receipt = pending.length === 0 && last?.senderId === me
    ? receiptLabel(conversation, last)
    : undefined;
  const first = messages[0];
  const own = pending.length > 0 || last?.senderId === me;

  return (
    <ScrollView
      ref={scroll}
      className="flex-1"
      contentContainerClassName="gap-2 px-3 pt-3 pb-4"
      keyboardShouldPersistTaps="handled"
      keyboardDismissMode="interactive"
      onScroll={(event) => {
        const { contentOffset, contentSize, layoutMeasurement } = event.nativeEvent;
        atBottom.current = contentOffset.y + layoutMeasurement.height >= contentSize.height - 120;
      }}
      scrollEventThrottle={64}
      // The newest stays in view as the thread grows, for the reader at the
      // bottom and always for their own sends.
      onContentSizeChange={() => {
        if (atBottom.current || own) scroll.current?.scrollToEnd({ animated: true });
      }}
    >
      {hasEarlier && first !== undefined && (
        <Earlier
          conversationId={conversation.id}
          beforeSeq={first.seq}
          me={me}
          named={named}
          staff={props.staff}
          now={now}
        />
      )}
      {items.length === 0 && pending.length === 0 && (
        <Text className="py-10 text-center text-muted-foreground">No messages yet.</Text>
      )}
      {items.map((item) => (
        <Item
          key={item.key}
          item={item}
          named={named}
          staff={props.staff}
          now={now}
          isFresh={(seq) => seq > openedAt}
        />
      ))}
      {pending.map((sending) => (
        <Pending
          key={sending.draft.clientMessageId}
          pending={sending}
          conversation={conversation}
        />
      ))}
      {receipt !== undefined && (
        <Text className="-mt-1 px-1 text-right text-xs text-muted-foreground">{receipt}</Text>
      )}
      {othersTyping && <TypingDots />}
    </ScrollView>
  );
};

const Item = (props: {
  readonly item: ThreadItem;
  readonly named: boolean;
  readonly staff: boolean;
  readonly now: number;
  readonly isFresh: (seq: number) => boolean;
}) => {
  const { item } = props;
  if (item._tag === "Time") {
    return (
      <Text className="pt-3 pb-1 text-center text-xs font-semibold text-muted-foreground tabular-nums">
        {whenLabel(item.at, props.now)}
      </Text>
    );
  }
  if (item.mine) {
    return (
      <View className="items-end gap-0.5">
        <Bubbles run={item} who="You" isFresh={props.isFresh} />
      </View>
    );
  }
  return <TheirRun run={item} named={props.named} staff={props.staff} isFresh={props.isFresh} />;
};

/** Their run: their face beside the last word, and their name above it where more than two speak. */
const TheirRun = (props: {
  readonly run: Run;
  readonly named: boolean;
  readonly staff: boolean;
  readonly isFresh: (seq: number) => boolean;
}) => {
  const who = usePersonName(props.run.senderId, roleName(props.run.senderRole, props.staff));
  return (
    <View className="flex-row items-end gap-2">
      <PersonAvatar userId={props.run.senderId} size="xs" />
      <View className="min-w-0 flex-1 items-start gap-0.5">
        {props.named && (
          <Text className="px-3 pb-0.5 text-xs font-semibold text-muted-foreground">{who}</Text>
        )}
        <Bubbles run={props.run} who={who} isFresh={props.isFresh} />
      </View>
    </View>
  );
};

const Bubbles = (props: {
  readonly run: Run;
  readonly who: string;
  readonly isFresh: (seq: number) => boolean;
}) => (
  <>
    {props.run.messages.map((message, index) => (
      <Bubble
        key={message.seq}
        mine={props.run.mine}
        first={index === 0}
        last={index === props.run.messages.length - 1}
        fresh={props.isFresh(message.seq)}
        label={`${props.who}: ${message.body}`}
      >
        {message.body}
      </Bubble>
    ))}
  </>
);

const ROUND = 20;
const TUCK = 8;

/**
 * One message. Consecutive bubbles from one sender tuck their corners on the
 * sender's side, so a run reads as one voice. A new one pops in — theirs with
 * a spring, yours rising from the composer.
 */
const Bubble = (props: {
  readonly mine: boolean;
  readonly first: boolean;
  readonly last: boolean;
  readonly fresh?: boolean;
  readonly sending?: boolean;
  readonly failed?: boolean;
  readonly label: string;
  readonly children: React.ReactNode;
}) => {
  const side = props.mine ? "Right" : "Left";
  const corners = {
    borderRadius: ROUND,
    [`borderTop${side}Radius`]: props.first ? ROUND : TUCK,
    [`borderBottom${side}Radius`]: props.last ? ROUND : TUCK,
  };
  return (
    <Animated.View
      {...(props.fresh === true ? { entering: props.mine ? mineIn : theirsIn } : {})}
      accessible
      accessibilityLabel={props.label}
      className={cn(
        "max-w-[82%] px-3.5 py-2",
        props.failed === true
          ? "bg-destructive/10"
          : props.mine
          ? "bg-secondary"
          : "bg-tile",
        props.sending === true && "opacity-60",
      )}
      style={corners}
    >
      <Text
        className={cn(
          "text-[15px] leading-[20px]",
          props.failed === true
            ? "text-destructive"
            : props.mine
            ? "text-secondary-foreground"
            : "text-foreground",
        )}
      >
        {props.children}
      </Text>
    </Animated.View>
  );
};

/** Why a send failed, in the words the failure carries. */
const reason = (error: SendError): string =>
  "message" in error && typeof error.message === "string" && error.message !== ""
    ? error.message
    : "it did not reach Surge";

const Pending = (
  props: { readonly pending: PendingMessage; readonly conversation: Conversation; },
) => {
  const send = useAtomSet(sendMessage);
  const { draft, failure } = props.pending;
  const body = draft.body !== ""
    ? draft.body
    : props.conversation.quickReplies.find((reply) => reply.code === draft.quickReply)?.text
      ?? draft.quickReply;
  const failed = Option.isSome(failure);

  return (
    <View className="items-end gap-1">
      <Bubble mine first last fresh sending={!failed} failed={failed} label={`You: ${body}`}>
        {body}
      </Bubble>
      {Option.match(failure, {
        onNone: () => <Text className="px-1 text-xs text-muted-foreground">Sending…</Text>,
        onSome: (error) => (
          <Pressable
            accessibilityRole="button"
            accessibilityLabel={`Not sent: ${reason(error)}. Try again`}
            onPress={() => {
              send({ conversationId: props.conversation.id, draft });
            }}
            className="px-1"
          >
            <Text accessibilityRole="alert" className="text-right text-xs text-destructive">
              Not sent: {reason(error)}.{" "}
              <Text className="text-xs font-semibold text-destructive underline">Try again</Text>
            </Text>
          </Pressable>
        ),
      })}
    </View>
  );
};

/** The other side typing: three dots breathing on their side. Presence, not news, so not announced. */
const TypingDots = () => (
  <Animated.View
    entering={theirsIn}
    accessibilityElementsHidden
    importantForAccessibility="no-hide-descendants"
    className="ml-9 flex-row items-center gap-1 self-start rounded-[20px] bg-tile px-3.5 py-3"
  >
    {[0, 1, 2].map((dot) => <Dot key={dot} delay={dot * 160} />)}
  </Animated.View>
);

const Dot = (props: { readonly delay: number; }) => {
  const opacity = useSharedValue(0.3);
  React.useEffect(() => {
    opacity.value = withDelay(
      props.delay,
      withRepeat(
        withSequence(
          withTiming(1, { duration: 360, reduceMotion: ReduceMotion.System }),
          withTiming(0.3, { duration: 360, reduceMotion: ReduceMotion.System }),
        ),
        -1,
      ),
    );
  }, [opacity, props.delay]);
  const breathing = useAnimatedStyle(() => ({ opacity: opacity.value }));
  return <Animated.View className="size-2 rounded-full bg-foreground/60" style={breathing} />;
};

/** A page of history before `beforeSeq`, on request, then the page before that above it. */
const Earlier = (props: {
  readonly conversationId: ConversationId;
  readonly beforeSeq: number;
  readonly me: UserId;
  readonly named: boolean;
  readonly staff: boolean;
  readonly now: number;
}) => {
  const [shown, setShown] = React.useState(false);
  if (!shown) {
    return (
      <Pressable
        accessibilityRole="button"
        onPress={() => {
          setShown(true);
        }}
        className="self-center rounded-full px-4 py-2 active:bg-tile"
      >
        <Text className="text-sm font-semibold">Show earlier messages</Text>
      </Pressable>
    );
  }
  return <EarlierPage {...props} />;
};

const EarlierPage = (props: {
  readonly conversationId: ConversationId;
  readonly beforeSeq: number;
  readonly me: UserId;
  readonly named: boolean;
  readonly staff: boolean;
  readonly now: number;
}) => {
  const page = useAtomValue(
    earlierMessagesAtom(
      EarlierKey.make({ conversationId: props.conversationId, beforeSeq: props.beforeSeq }),
    ),
  );
  if (AsyncResult.isInitial(page)) {
    return (
      <Text className="py-2 text-center text-muted-foreground">Loading earlier messages…</Text>
    );
  }
  if (AsyncResult.isFailure(page)) return <QueryError result={page} subject="earlier messages" />;
  const { messages, hasMore } = page.value;
  const first = messages[0];
  return (
    <View className="gap-2">
      {hasMore && first !== undefined && <Earlier {...props} beforeSeq={first.seq} />}
      {threadItems(messages, props.me).map((item) => (
        <Item
          key={`earlier-${item.key}`}
          item={item}
          named={props.named}
          staff={props.staff}
          now={props.now}
          isFresh={() => false}
        />
      ))}
    </View>
  );
};

/**
 * Moves the reader's marker to the newest message while the app is in front,
 * and again when it comes back to the front. Only a participant has a marker.
 */
const useMarkRead = (conversation: Conversation, messages: ReadonlyArray<Message>, me: UserId) => {
  const read = useAtomSet(markRead);
  const newest = messages.at(-1)?.seq ?? 0;
  const held = conversation.participants.find((participant) => participant.userId === me)
    ?.lastReadSeq;
  const asked = React.useRef(0);
  const conversationId = conversation.id;

  React.useEffect(() => {
    if (held === undefined) return;
    const mark = () => {
      if (AppState.currentState !== "active") return;
      if (newest <= held || newest <= asked.current) return;
      asked.current = newest;
      read({ conversationId, seq: newest });
    };
    mark();
    const subscription = AppState.addEventListener("change", mark);
    return () => {
      subscription.remove();
    };
  }, [conversationId, newest, held, read]);
};
