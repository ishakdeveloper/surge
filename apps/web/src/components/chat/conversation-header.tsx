import { sessionAtom } from "@/atom/session-atoms.js";
import { PersonAvatar, PersonName } from "@/components/profile/avatar.js";
import { useAtomValue } from "@effect/atom-react";
import { conversationAtom, typingAtom } from "@surge/common/atom/chat-atoms";
import { conversationTitle, roleName, statusLine } from "@surge/common/chat/thread";
import type { ConversationId, UserId } from "@surge/domain/api/Primitives";
import type { Conversation, Participant } from "@surge/domain/chat/Chat";
import { AsyncResult } from "effect/unstable/reactivity";
import { LifeBuoy } from "lucide-react";
import type * as React from "react";

/**
 * Whose face the header shows: the other side of a trip, and for support the
 * person who asked. Support itself has no one face, so the person asking sees
 * the lifebuoy instead.
 */
const counterpartOf = (
  conversation: Conversation,
  me: UserId,
  staff: boolean,
): Participant | undefined =>
  conversation.kind === "CONVERSATION_KIND_SUPPORT"
    ? staff
      ? conversation.participants.find((participant) =>
        participant.userId === conversation.requesterId
      )
      : undefined
    : staff
    ? undefined
    : conversation.participants.find((participant) => participant.userId !== me);

/**
 * A conversation's title and what it is doing now — "Typing…" while the other
 * side is — with a way back on the left and, for support, what can be done to
 * it on the right.
 */
export const ConversationHeader = (props: {
  readonly conversationId: ConversationId;
  readonly staff?: boolean;
  readonly back: React.ReactNode;
  readonly actions?: (conversation: Conversation) => React.ReactNode;
}) => {
  const view = useAtomValue(conversationAtom(props.conversationId));
  const typing = useAtomValue(typingAtom(props.conversationId));
  const me = useAtomValue(
    sessionAtom,
    (session) => AsyncResult.isSuccess(session) ? session.value.userId : undefined,
  );
  const staff = props.staff === true;
  const conversation = AsyncResult.isSuccess(view) ? view.value.conversation : undefined;
  const othersTyping = AsyncResult.isSuccess(typing) && AsyncResult.isSuccess(view)
    && typing.value.some((userId) => userId !== view.value.me);

  const other = conversation === undefined || me === undefined
    ? undefined
    : counterpartOf(conversation, me, staff);

  return (
    <header className="flex items-center gap-3">
      {props.back}
      {other !== undefined && <PersonAvatar userId={other.userId} size="sm" />}
      {conversation?.kind === "CONVERSATION_KIND_SUPPORT" && !staff && (
        <span
          aria-hidden
          className="grid size-9 shrink-0 place-items-center rounded-full bg-tile [&_svg]:size-4"
        >
          <LifeBuoy />
        </span>
      )}
      <div className="flex min-w-0 flex-1 flex-col">
        <h1 className="truncate text-lg leading-tight font-bold">
          {conversation === undefined || me === undefined
            ? "Conversation"
            : other !== undefined && conversation.kind === "CONVERSATION_KIND_TRIP"
            ? <PersonName userId={other.userId} role={roleName(other.role, staff)} />
            : conversationTitle(conversation, me, staff)}
        </h1>
        <p className="truncate text-[13px] text-muted-foreground">
          {othersTyping
            ? "Typing…"
            : conversation === undefined || me === undefined
            ? " "
            : statusLine(conversation, me, staff)}
        </p>
      </div>
      {conversation !== undefined && props.actions?.(conversation)}
    </header>
  );
};
