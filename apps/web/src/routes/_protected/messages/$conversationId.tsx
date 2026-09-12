import { ActionError } from "@/components/app/action-error.js";
import { ChatComposer } from "@/components/chat/chat-composer.js";
import { ChatThread } from "@/components/chat/chat-thread.js";
import { ConversationHeader } from "@/components/chat/conversation-header.js";
import { actionVariants } from "@/components/sign/sign.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { resolveConversation } from "@surge/common/atom/chat-atoms";
import { ConversationId } from "@surge/domain/api/Primitives";
import { isOpen } from "@surge/domain/chat/Chat";
import { createFileRoute, Link, useParams } from "@tanstack/react-router";
import { AsyncResult } from "effect/unstable/reactivity";
import { ArrowLeft } from "lucide-react";

/**
 * One conversation, as a page: its title and state, the thread scrolling in
 * the middle, and the composer at the foot of the card where the thumb is.
 * Whoever asked support for help can mark it resolved from here.
 */
const ConversationPage = () => {
  // Not strict: the page is also mounted outside its route by the page tests.
  const params = useParams({ strict: false });
  if (params.conversationId === undefined) {
    return <p className="text-[15px] text-muted-foreground">No conversation to show.</p>;
  }
  const conversationId = ConversationId.make(params.conversationId);

  return (
    <div className="mx-auto flex min-h-0 w-full max-w-xl flex-1 flex-col overflow-hidden rounded-3xl bg-card shadow-[0_1px_2px_rgb(0_0_0/0.04),0_12px_32px_-18px_rgb(0_0_0/0.22)]">
      <div className="px-4 pt-4 pb-3">
        <ConversationHeader
          conversationId={conversationId}
          back={
            <Link
              to="/messages"
              aria-label="Back to messages"
              className="grid size-9 shrink-0 place-items-center rounded-full bg-tile transition-colors duration-150 hover:bg-tile-hover"
            >
              <ArrowLeft aria-hidden className="size-4" />
            </Link>
          }
          actions={(conversation) =>
            conversation.kind === "CONVERSATION_KIND_SUPPORT" && isOpen(conversation)
              ? <Resolve conversationId={conversationId} />
              : null}
        />
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-4 pb-2">
        <ChatThread conversationId={conversationId} />
      </div>
      <div className="px-4 pt-2 pb-4">
        <ChatComposer conversationId={conversationId} autoFocus />
      </div>
    </div>
  );
};

/** Ending a support conversation the reader opened: it stays to read, and takes no more. */
const Resolve = (props: { readonly conversationId: ConversationId; }) => {
  const resolving = useAtomValue(resolveConversation);
  const resolve = useAtomSet(resolveConversation);
  return (
    <div className="flex flex-col items-end gap-1">
      <button
        type="button"
        disabled={resolving.waiting}
        onClick={() => {
          resolve(props.conversationId);
        }}
        className={actionVariants({ tone: "quiet", size: "sm" })}
      >
        {resolving.waiting ? "Resolving…" : "Mark resolved"}
      </button>
      {AsyncResult.isFailure(resolving) && <ActionError cause={resolving.cause} />}
    </div>
  );
};

export const Route = createFileRoute("/_protected/messages/$conversationId")({
  ssr: false,
  staticData: { crumb: "Conversation" },
  component: ConversationPage,
});
