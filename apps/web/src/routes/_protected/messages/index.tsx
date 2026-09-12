import { sessionAtom } from "@/atom/session-atoms.js";
import { QueryError } from "@/components/app/query-error.js";
import { UnreadCount, unreadWords } from "@/components/chat/unread-count.js";
import { PersonAvatar, PersonName } from "@/components/profile/avatar.js";
import { actionVariants, IconBubble, pressable, signVariants } from "@/components/sign/sign.js";
import { cn } from "@/lib/utils.js";
import { useAtomMount, useAtomValue } from "@effect/atom-react";
import { chatPushesAtom, conversationsAtom } from "@surge/common/atom/chat-atoms";
import { conversationTitle, roleName, statusLine, whenLabel } from "@surge/common/chat/thread";
import type { UserId } from "@surge/domain/api/Primitives";
import type { Conversation } from "@surge/domain/chat/Chat";
import { createFileRoute, Link } from "@tanstack/react-router";
import { AsyncResult } from "effect/unstable/reactivity";
import { LifeBuoy } from "lucide-react";
import * as React from "react";

/**
 * Everything a rider or driver has said and been told: the chat with the
 * driver or rider of each recent trip, and their conversations with support —
 * newest first, what is unread counted in yellow. Asking support for help
 * starts here too.
 */
const Messages = () => {
  useAtomMount(chatPushesAtom);
  const conversations = useAtomValue(conversationsAtom);
  const me = useAtomValue(
    sessionAtom,
    (session) => AsyncResult.isSuccess(session) ? session.value.userId : undefined,
  );

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
      <div className="mx-auto flex w-full max-w-md flex-col gap-4 rounded-3xl bg-card p-5 shadow-[0_1px_2px_rgb(0_0_0/0.04),0_12px_32px_-18px_rgb(0_0_0/0.22)]">
        <header className="flex flex-col gap-1 px-1">
          <h1 className="text-2xl font-bold">Messages</h1>
          <p className="text-[15px] text-pretty text-muted-foreground">
            Chats with the other side of your trips, and your conversations with Surge support.
          </p>
        </header>

        {AsyncResult.isInitial(conversations)
          ? <p className="px-1 py-6 text-sm text-muted-foreground">Loading your messages…</p>
          : AsyncResult.isFailure(conversations)
          ? <QueryError result={conversations} subject="your messages" />
          : conversations.value.length === 0 || me === undefined
          ? (
            <p className="rounded-2xl bg-tile px-4 py-3.5 text-[15px] text-pretty text-muted-foreground">
              Nothing yet. When a trip is accepted, you can message the other side of it from the
              trip itself, and the conversation shows up here too.
            </p>
          )
          : <ConversationList conversations={conversations.value} me={me} />}

        <section aria-labelledby="help" className="flex flex-col gap-2 px-1 pt-2">
          <h2 id="help" className="text-base font-semibold">Need a hand?</h2>
          <p className="text-[15px] text-pretty text-muted-foreground">
            Something wrong with a trip, a fare or your account? Surge support replies here.
          </p>
          <Link to="/messages/new" className={cn(actionVariants({ size: "block" }), "mt-1")}>
            Contact support
          </Link>
        </section>
      </div>
    </div>
  );
};

const ConversationList = (props: {
  readonly conversations: ReadonlyArray<Conversation>;
  readonly me: UserId;
}) => {
  // oxlint-disable-next-line effecttsgo/global-date
  const [now] = React.useState(() => Date.now());
  return (
    <ul className="flex flex-col gap-2">
      {props.conversations.map((conversation) => (
        <li key={conversation.id}>
          <Row conversation={conversation} me={props.me} now={now} />
        </li>
      ))}
    </ul>
  );
};

/**
 * One conversation: who it is with — for a trip, their face and first name —
 * what it is doing, and what is unread.
 */
const Row = (props: {
  readonly conversation: Conversation;
  readonly me: UserId;
  readonly now: number;
}) => {
  const { conversation } = props;
  const other = conversation.kind === "CONVERSATION_KIND_TRIP"
    ? conversation.participants.find((participant) => participant.userId !== props.me)
    : undefined;

  return (
    <Link
      to="/messages/$conversationId"
      params={{ conversationId: conversation.id }}
      className={cn(signVariants({ tone: "choice" }), pressable, "items-center")}
    >
      {other === undefined
        ? (
          <IconBubble>
            <LifeBuoy />
          </IconBubble>
        )
        : <PersonAvatar userId={other.userId} />}
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="truncate text-base font-semibold">
          {other === undefined
            ? conversationTitle(conversation, props.me, false)
            : <PersonName userId={other.userId} role={roleName(other.role, false)} />}
        </span>
        <span className="truncate text-[13px] text-muted-foreground">
          {conversation.unread > 0 && (
            <span className="sr-only">{unreadWords(conversation.unread)}.</span>
          )}
          {statusLine(conversation, props.me, false)} ·{" "}
          {whenLabel(conversation.updatedAt, props.now)}
        </span>
      </span>
      {conversation.unread > 0 && <UnreadCount count={conversation.unread} />}
    </Link>
  );
};

export const Route = createFileRoute("/_protected/messages/")({
  // Client-only: the list moves with the socket's doorbells.
  ssr: false,
  staticData: { crumb: "Messages" },
  component: Messages,
});
