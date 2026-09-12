import { QueryError } from "@/components/app/query-error.js";
import { PersonAvatar, usePersonName } from "@/components/profile/avatar.js";
import { actionVariants } from "@/components/sign/sign.js";
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
  formatClock,
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

/**
 * A conversation's messages, live.
 *
 * Runs of bubbles by sender — yours in ink on the right, theirs on grey tiles
 * on the left — with a time wherever the conversation paused. The newest of
 * your own is marked Sent or Seen; a send shows the moment it is made, faded
 * until the server has it, and a failed one says why and offers itself again
 * with the same key, so a retry is never a second message. Reading the thread
 * marks it read. The composer is separate, so a page can pin it under the
 * thumb and let this scroll.
 */
export const ChatThread = (
  props: { readonly conversationId: ConversationId; readonly staff?: boolean; },
) => {
  const view = useAtomValue(conversationAtom(props.conversationId));
  if (AsyncResult.isInitial(view)) {
    return (
      <p className="px-1 py-8 text-center text-sm text-muted-foreground">
        Opening the conversation…
      </p>
    );
  }
  if (AsyncResult.isFailure(view)) {
    return <QueryError result={view} subject="this conversation" />;
  }
  return <Thread view={view.value} staff={props.staff === true} />;
};

const Thread = (props: { readonly view: ConversationView; readonly staff: boolean; }) => {
  const { conversation, messages, pending, me, hasEarlier } = props.view;
  const typing = useAtomValue(typingAtom(conversation.id));
  const othersTyping = AsyncResult.isSuccess(typing)
    && typing.value.some((userId) => userId !== me);
  // The day boundary the time labels are judged against, fixed for the life
  // of the thread rather than ticking it into a re-render every second.
  // oxlint-disable-next-line effecttsgo/global-date
  const [now] = React.useState(() => Date.now());
  // What was here when the thread opened arrives without a pop; only what
  // lands while it is open animates in.
  const [openedAt] = React.useState(() => messages.at(-1)?.seq ?? 0);
  const named = props.staff || conversation.kind === "CONVERSATION_KIND_SUPPORT";
  const items = React.useMemo(() => threadItems(messages, me), [messages, me]);
  const last = messages.at(-1);

  useMarkRead(conversation, messages, me);
  const bottom = useStickToBottom(
    messages.length + pending.length,
    pending.length > 0 || last?.senderId === me,
  );

  const receipt = pending.length === 0 && last !== undefined && last.senderId === me
    ? receiptLabel(conversation, last)
    : undefined;
  const first = messages[0];

  return (
    <div className="flex flex-col gap-2">
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
      {
        /* A log: assistive tech reads out what is added to it, which for a
          conversation is exactly the news worth hearing. */
      }
      <div role="log" aria-label="Messages" className="flex flex-col gap-2">
        {items.length === 0 && pending.length === 0 && (
          <p className="py-8 text-center text-sm text-muted-foreground">No messages yet.</p>
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
      </div>
      {receipt !== undefined && (
        <p className="-mt-1 px-1 text-right text-xs text-muted-foreground">{receipt}</p>
      )}
      {othersTyping && <TypingBubble />}
      <div ref={bottom} aria-hidden className="h-px" />
    </div>
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
      <p className="pt-3 pb-1 text-center text-xs font-semibold text-muted-foreground tabular-nums">
        {whenLabel(item.at, props.now)}
      </p>
    );
  }
  if (item.mine) {
    return (
      <div className="flex flex-col items-end gap-0.5">
        <Bubbles run={item} who="You" isFresh={props.isFresh} />
      </div>
    );
  }
  return <TheirRun run={item} named={props.named} staff={props.staff} isFresh={props.isFresh} />;
};

type Run = Extract<ThreadItem, { readonly _tag: "Run"; }>;

/**
 * Their run: their face beside it at the foot, where the last word is, and —
 * where more than two people can speak — their name above it.
 */
const TheirRun = (props: {
  readonly run: Run;
  readonly named: boolean;
  readonly staff: boolean;
  readonly isFresh: (seq: number) => boolean;
}) => {
  const who = usePersonName(props.run.senderId, roleName(props.run.senderRole, props.staff));
  return (
    <div className="flex items-end gap-2">
      <PersonAvatar userId={props.run.senderId} size="xs" className="mb-px" />
      <div className="flex min-w-0 flex-1 flex-col items-start gap-0.5">
        {props.named && (
          <span aria-hidden className="px-3 pb-0.5 text-xs font-semibold text-muted-foreground">
            {who}
          </span>
        )}
        <Bubbles run={props.run} who={who} isFresh={props.isFresh} />
      </div>
    </div>
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
        title={formatClock(message.createdAt)}
      >
        <span className="sr-only">{props.who}:</span>
        {message.body}
      </Bubble>
    ))}
  </>
);

/**
 * One message. Consecutive bubbles from one sender tuck their corners on the
 * sender's side, so a run reads as one voice.
 */
const Bubble = (props: {
  readonly mine: boolean;
  readonly first: boolean;
  readonly last: boolean;
  readonly fresh?: boolean;
  readonly sending?: boolean;
  readonly failed?: boolean;
  readonly title?: string;
  readonly children: React.ReactNode;
}) => (
  <p
    title={props.title}
    className={cn(
      "max-w-[82%] rounded-[20px] px-3.5 py-2 text-[15px] leading-snug whitespace-pre-wrap [overflow-wrap:anywhere]",
      props.mine ? "bg-secondary text-secondary-foreground" : "bg-tile text-foreground",
      props.mine
        ? [!props.first && "rounded-tr-sm", !props.last && "rounded-br-sm"]
        : [!props.first && "rounded-tl-sm", !props.last && "rounded-bl-sm"],
      props.fresh === true
        && (props.mine ? "motion-safe:animate-rise-in" : "motion-safe:animate-pop-in"),
      props.sending === true && "opacity-60",
      props.failed === true && "bg-destructive/10 text-destructive",
    )}
  >
    {props.children}
  </p>
);

/** Why a send failed, in the words the failure carries. */
const reason = (error: SendError): string =>
  "message" in error && typeof error.message === "string" && error.message !== ""
    ? error.message
    : "it did not reach Surge";

/** A send the server does not have yet: fading while it travels, and offered again if it failed. */
const Pending = (
  props: { readonly pending: PendingMessage; readonly conversation: Conversation; },
) => {
  const send = useAtomSet(sendMessage);
  const { draft, failure } = props.pending;
  // A quick reply is sent as its code; until the server answers with the
  // stored message, its text comes from the conversation's own catalogue.
  const body = draft.body !== ""
    ? draft.body
    : props.conversation.quickReplies.find((reply) => reply.code === draft.quickReply)?.text
      ?? draft.quickReply;
  const failed = Option.isSome(failure);

  return (
    <div className="flex flex-col items-end gap-1">
      <Bubble mine first last sending={!failed} failed={failed}>
        <span className="sr-only">You:</span>
        {body}
      </Bubble>
      {Option.match(failure, {
        onNone: () => <p className="px-1 text-xs text-muted-foreground">Sending…</p>,
        onSome: (error) => (
          <p role="alert" className="px-1 text-right text-xs text-destructive">
            Not sent: {reason(error)}.{" "}
            <button
              type="button"
              onClick={() => {
                send({ conversationId: props.conversation.id, draft });
              }}
              className="cursor-pointer font-semibold underline underline-offset-2"
            >
              Try again
            </button>
          </p>
        ),
      })}
    </div>
  );
};

/** The other side typing: three dots on their side of the thread. Not announced; it is presence, not news. */
const TypingBubble = () => (
  <div
    aria-hidden
    className="flex w-fit items-center gap-1 rounded-[20px] bg-tile px-3.5 py-3 motion-safe:animate-pop-in"
  >
    {[0, 160, 320].map((delay) => (
      <span
        key={delay}
        className="size-1.5 rounded-full bg-foreground/45 motion-safe:animate-pulse"
        style={{ animationDelay: `${delay}ms` }}
      />
    ))}
  </div>
);

/** A page of history before `beforeSeq`, asked for, then the page before that, above it. */
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
      <button
        type="button"
        onClick={() => {
          setShown(true);
        }}
        className={cn(actionVariants({ tone: "quiet", size: "sm" }), "self-center")}
      >
        Show earlier messages
      </button>
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
      <p className="py-2 text-center text-sm text-muted-foreground">Loading earlier messages…</p>
    );
  }
  if (AsyncResult.isFailure(page)) return <QueryError result={page} subject="earlier messages" />;

  const { messages, hasMore } = page.value;
  const first = messages[0];
  return (
    <div className="flex flex-col gap-2">
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
    </div>
  );
};

/**
 * Moves the reader's marker to the newest message while the thread is on
 * screen and the tab is in front. Only a participant has a marker; support
 * reading a conversation it is not in leaves it alone.
 */
const useMarkRead = (
  conversation: Conversation,
  messages: ReadonlyArray<Message>,
  me: UserId,
) => {
  const read = useAtomSet(markRead);
  const newest = messages.at(-1)?.seq ?? 0;
  const held = conversation.participants.find((participant) => participant.userId === me)
    ?.lastReadSeq;
  const asked = React.useRef(0);
  const conversationId = conversation.id;

  React.useEffect(() => {
    if (held === undefined) return;
    const mark = () => {
      if (document.visibilityState !== "visible") return;
      if (newest <= held || newest <= asked.current) return;
      asked.current = newest;
      read({ conversationId, seq: newest });
    };
    mark();
    document.addEventListener("visibilitychange", mark);
    return () => {
      document.removeEventListener("visibilitychange", mark);
    };
  }, [conversationId, newest, held, read]);
};

/**
 * Keeps the newest message in view as the thread grows, while the reader is
 * at the bottom — and always for their own sends. Someone scrolled back to
 * read is left where they are.
 */
const useStickToBottom = (count: number, own: boolean) => {
  const bottom = React.useRef<HTMLDivElement>(null);
  const near = React.useRef(true);
  const opened = React.useRef(false);

  React.useEffect(() => {
    const element = bottom.current;
    if (element === null || typeof IntersectionObserver === "undefined") return;
    const observer = new IntersectionObserver(
      ([entry]) => {
        near.current = entry?.isIntersecting ?? false;
      },
      { rootMargin: "0px 0px 160px 0px" },
    );
    observer.observe(element);
    return () => {
      observer.disconnect();
    };
  }, []);

  React.useLayoutEffect(() => {
    const element = bottom.current;
    if (element === null || typeof element.scrollIntoView !== "function") return;
    if (!opened.current) {
      opened.current = true;
      element.scrollIntoView({ block: "end" });
      return;
    }
    if (!near.current && !own) return;
    const still = globalThis.matchMedia("(prefers-reduced-motion: reduce)").matches;
    element.scrollIntoView({ block: "end", behavior: still ? "auto" : "smooth" });
  }, [count, own]);

  return bottom;
};
