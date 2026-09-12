import { sessionAtom } from "@/atom/session-atoms.js";
import { ActionError } from "@/components/app/action-error.js";
import { QueryError } from "@/components/app/query-error.js";
import { ChatComposer } from "@/components/chat/chat-composer.js";
import { ChatThread } from "@/components/chat/chat-thread.js";
import { ConversationHeader } from "@/components/chat/conversation-header.js";
import { UnreadCount, unreadWords } from "@/components/chat/unread-count.js";
import { actionVariants, IconBubble, pressable, signVariants } from "@/components/sign/sign.js";
import { cn } from "@/lib/utils.js";
import { useAtomMount, useAtomSet, useAtomValue } from "@effect/atom-react";
import {
  chatPushesAtom,
  claimConversation,
  conversationAtom,
  resolveConversation,
  supportQueueAtom,
} from "@surge/common/atom/chat-atoms";
import { conversationTitle, counterpartName, whenLabel } from "@surge/common/chat/thread";
import { ConversationId, type UserId } from "@surge/domain/api/Primitives";
import { type Conversation, isOpen } from "@surge/domain/chat/Chat";
import { createFileRoute, Link, useSearch } from "@tanstack/react-router";
import { AsyncResult } from "effect/unstable/reactivity";
import { ArrowLeft, LifeBuoy } from "lucide-react";
import * as React from "react";

/**
 * Support's desk: every open support conversation on the left, oldest need
 * first to the eye by its yellow "New", and the one chosen on the right with
 * its thread and composer. Taking a conversation claims it; replying to an
 * unclaimed one claims it too, and a second agent is refused by the server,
 * so the page only offers the composer to whoever can use it.
 *
 * The chosen conversation is in the URL, so a link to it can be passed on.
 */
const Support = () => {
  useAtomMount(chatPushesAtom);
  const search = useSearch({ strict: false });
  const chosen = typeof search.c === "string" && search.c !== ""
    ? ConversationId.make(search.c)
    : undefined;
  const queue = useAtomValue(supportQueueAtom);
  const me = useAtomValue(
    sessionAtom,
    (session) => AsyncResult.isSuccess(session) ? session.value.userId : undefined,
  );

  return (
    <div className="flex min-h-0 flex-1 gap-4">
      <section
        aria-labelledby="support-queue"
        className={cn(
          "flex min-h-0 w-full flex-col gap-3 overflow-y-auto lg:w-80 lg:shrink-0",
          chosen !== undefined && "max-lg:hidden",
        )}
      >
        <header className="flex flex-col gap-1 px-1">
          <h1 id="support-queue" className="text-2xl font-bold">Support</h1>
          <p className="text-[15px] text-muted-foreground">
            {AsyncResult.isSuccess(queue)
              ? queue.value.length === 0
                ? "Nobody is waiting."
                : `${queue.value.length} open ${
                  queue.value.length === 1 ? "conversation" : "conversations"
                }`
              : "Open conversations with riders and drivers."}
          </p>
        </header>
        {AsyncResult.isInitial(queue)
          ? <p className="px-1 text-sm text-muted-foreground">Loading the queue…</p>
          : AsyncResult.isFailure(queue)
          ? <QueryError result={queue} subject="the support queue" />
          : me === undefined
          ? null
          : <Queue conversations={queue.value} me={me} chosen={chosen} />}
      </section>

      {chosen === undefined
        ? (
          <p className="hidden flex-1 place-items-center rounded-3xl bg-card/70 text-[15px] text-muted-foreground lg:grid">
            Choose a conversation from the queue.
          </p>
        )
        : me === undefined
        ? null
        : <Desk key={chosen} conversationId={chosen} me={me} />}
    </div>
  );
};

const Queue = (props: {
  readonly conversations: ReadonlyArray<Conversation>;
  readonly me: UserId;
  readonly chosen: ConversationId | undefined;
}) => {
  // oxlint-disable-next-line effecttsgo/global-date
  const [now] = React.useState(() => Date.now());
  return (
    <ul className="flex flex-col gap-2">
      {props.conversations.map((conversation) => {
        const current = conversation.id === props.chosen;
        return (
          <li key={conversation.id}>
            <Link
              to="/support"
              search={{ c: conversation.id }}
              aria-current={current ? "page" : undefined}
              className={cn(
                signVariants({ tone: "choice" }),
                pressable,
                "items-center",
                // The one open on the right lifts white with an ink ring, as a
                // field does when it takes focus.
                current && "bg-card shadow-[inset_0_0_0_2px_var(--foreground)] hover:bg-card",
              )}
            >
              <IconBubble>
                <LifeBuoy />
              </IconBubble>
              <span className="flex min-w-0 flex-1 flex-col">
                <span className="truncate text-base font-semibold">
                  {conversationTitle(conversation, props.me, true)}
                </span>
                <span className="truncate text-[13px] text-muted-foreground">
                  {conversation.unread > 0 && (
                    <span className="sr-only">{unreadWords(conversation.unread)}.</span>
                  )}
                  {counterpartName(conversation, props.me, true)} ·{" "}
                  {whenLabel(conversation.updatedAt, now)}
                </span>
              </span>
              <Holder conversation={conversation} me={props.me} />
              {conversation.unread > 0 && <UnreadCount count={conversation.unread} />}
            </Link>
          </li>
        );
      })}
    </ul>
  );
};

/** Who has it: yellow "New" when nobody does — taking it is the next step — ink when it is yours. */
const Holder = (props: { readonly conversation: Conversation; readonly me: UserId; }) => {
  const { assigneeId } = props.conversation;
  if (assigneeId === "") {
    return (
      <span className="shrink-0 rounded-full bg-primary px-2.5 py-1 text-xs font-bold text-primary-foreground">
        New
      </span>
    );
  }
  return assigneeId === props.me
    ? (
      <span className="shrink-0 rounded-full bg-secondary px-2.5 py-1 text-xs font-bold text-secondary-foreground">
        Yours
      </span>
    )
    : <span className="shrink-0 text-xs font-semibold text-muted-foreground">Taken</span>;
};

/** The chosen conversation: its header with take and resolve, the thread, and the composer if it is the reader's to answer. */
const Desk = (props: { readonly conversationId: ConversationId; readonly me: UserId; }) => {
  const view = useAtomValue(conversationAtom(props.conversationId));
  const claiming = useAtomValue(claimConversation);
  const claim = useAtomSet(claimConversation);
  const resolving = useAtomValue(resolveConversation);
  const resolve = useAtomSet(resolveConversation);
  const conversation = AsyncResult.isSuccess(view) ? view.value.conversation : undefined;
  const takenByOther = conversation !== undefined && conversation.assigneeId !== ""
    && conversation.assigneeId !== props.me;

  return (
    <section
      aria-label="Conversation"
      className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-3xl bg-card shadow-[0_1px_2px_rgb(0_0_0/0.04),0_12px_32px_-18px_rgb(0_0_0/0.22)]"
    >
      <div className="flex flex-col gap-2 px-4 pt-4 pb-3">
        <ConversationHeader
          conversationId={props.conversationId}
          staff
          back={
            <Link
              to="/support"
              search={{}}
              aria-label="Back to the queue"
              className="grid size-9 shrink-0 place-items-center rounded-full bg-tile transition-colors duration-150 hover:bg-tile-hover lg:hidden"
            >
              <ArrowLeft aria-hidden className="size-4" />
            </Link>
          }
          actions={(current) =>
            !isOpen(current)
              ? null
              : current.assigneeId === ""
              ? (
                <button
                  type="button"
                  disabled={claiming.waiting}
                  onClick={() => {
                    claim(props.conversationId);
                  }}
                  className={actionVariants({ size: "sm" })}
                >
                  {claiming.waiting ? "Taking…" : "Take it"}
                </button>
              )
              : current.assigneeId === props.me
              ? (
                <button
                  type="button"
                  disabled={resolving.waiting}
                  onClick={() => {
                    resolve(props.conversationId);
                  }}
                  className={actionVariants({ tone: "quiet", size: "sm" })}
                >
                  {resolving.waiting ? "Resolving…" : "Resolve"}
                </button>
              )
              : null}
        />
        {AsyncResult.isFailure(claiming) && <ActionError cause={claiming.cause} />}
        {AsyncResult.isFailure(resolving) && <ActionError cause={resolving.cause} />}
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-4 pb-2">
        <ChatThread conversationId={props.conversationId} staff />
      </div>
      <div className="px-4 pt-2 pb-4">
        {takenByOther
          ? (
            <p className="rounded-2xl bg-tile px-4 py-3 text-sm text-muted-foreground">
              Someone else from support has this one, so only they can reply.
            </p>
          )
          : <ChatComposer conversationId={props.conversationId} staff />}
      </div>
    </section>
  );
};

export const Route = createFileRoute("/_protected/support/")({
  ssr: false,
  validateSearch: (search: Record<string, unknown>): { readonly c?: string | undefined; } =>
    typeof search["c"] === "string" && search["c"] !== "" ? { c: search["c"] } : {},
  staticData: { crumb: "Support" },
  component: Support,
});
