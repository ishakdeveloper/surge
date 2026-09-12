import { QueryError } from "@/components/app/query-error.js";
import { unreadWords } from "@/components/chat/unread-count.js";
import { PersonAvatar, usePersonName } from "@/components/profile/avatar.js";
import { actionVariants, signVariants } from "@/components/sign/sign.js";
import { cn } from "@/lib/utils.js";
import { useAtomMount, useAtomValue } from "@effect/atom-react";
import { chatPushesAtom, tripConversationAtom } from "@surge/common/atom/chat-atoms";
import { formatClock } from "@surge/common/chat/thread";
import type { ConversationId, TripId, UserId } from "@surge/domain/api/Primitives";
import { type Conversation, isOpen } from "@surge/domain/chat/Chat";
import { Link } from "@tanstack/react-router";
import { AsyncResult } from "effect/unstable/reactivity";
import { MessageCircle } from "lucide-react";

/**
 * The person on the other side of a trip, put in front of you the way a
 * rider expects: their face, their first name, what the trip is doing, and a
 * way to message them — what a rider waiting at a kerb, or a driver circling
 * the block, most often needs next.
 */
export const CounterpartCard = (props: {
  readonly tripId: TripId;
  readonly userId: UserId;
  /** Who they are until they give a name: "Your driver", "Your rider". */
  readonly role: string;
  /** What the trip is doing, or why to write. */
  readonly detail: string;
  /**
   * Opens the conversation where the card stands. Without it the button is a
   * link to the conversation's own page.
   */
  readonly onMessage?: () => void;
}) => {
  // The unread count moves with pushes, on whatever page the card sits.
  useAtomMount(chatPushesAtom);
  const name = usePersonName(props.userId, props.role);
  const conversation = useAtomValue(tripConversationAtom(props.tripId));

  return (
    <div className="flex flex-col gap-2">
      <div className={cn(signVariants({ tone: "choice" }), "items-center")}>
        <PersonAvatar userId={props.userId} size="lg" />
        <div className="flex min-w-0 flex-1 flex-col">
          <p className="truncate text-lg leading-tight font-bold">{name}</p>
          <p className="truncate text-[13px] text-muted-foreground">
            {name === props.role ? props.detail : `${props.role} · ${props.detail}`}
          </p>
        </div>
        {AsyncResult.isInitial(conversation)
          ? (
            <button
              type="button"
              disabled
              aria-busy
              className={cn(actionVariants({ size: "md" }), "shrink-0")}
            >
              <MessageCircle className="size-4" aria-hidden />
              Message
            </button>
          )
          : AsyncResult.isSuccess(conversation) && isOpen(conversation.value)
          ? (
            <MessageButton
              conversation={conversation.value}
              name={name}
              {...(props.onMessage === undefined ? {} : { onMessage: props.onMessage })}
            />
          )
          : null}
      </div>
      {AsyncResult.isFailure(conversation) && (
        <QueryError result={conversation} subject={`messages with ${name}`} />
      )}
    </div>
  );
};

/** Yellow, because writing to them is the next step; what is unread rides on it in ink. */
const MessageButton = (props: {
  readonly conversation: Conversation;
  readonly name: string;
  readonly onMessage?: () => void;
}) => {
  const { unread } = props.conversation;
  const className = cn(actionVariants({ size: "md" }), "shrink-0 gap-2", unread > 0 && "pr-2");
  const label = (
    <>
      <MessageCircle className="size-4" aria-hidden />
      Message
      <span className="sr-only">
        {" "}
        {props.name}
        {unread > 0 && `, ${unreadWords(unread)}`}
      </span>
      {unread > 0 && (
        <span
          aria-hidden
          className="grid h-6 min-w-6 place-items-center rounded-full bg-secondary px-1.5 text-xs font-bold text-secondary-foreground tabular-nums motion-safe:animate-pop-in"
        >
          {unread > 99 ? "99+" : unread}
        </span>
      )}
    </>
  );

  return props.onMessage === undefined
    ? (
      <Link
        to="/messages/$conversationId"
        params={{ conversationId: props.conversation.id satisfies ConversationId }}
        className={className}
      >
        {label}
      </Link>
    )
    : (
      <button type="button" onClick={props.onMessage} className={className}>
        {label}
      </button>
    );
};

/**
 * After a trip, the driver is still in reach for as long as the conversation
 * takes messages — an hour after the trip, for the phone left on the back
 * seat. Once it has closed, or while it is being read, this shows nothing: it
 * is a convenience beside the receipt, and the conversation itself stays in
 * Messages either way.
 */
export const AfterTripChat = (props: {
  readonly tripId: TripId;
  readonly userId: UserId;
  readonly role: string;
}) => {
  const conversation = useAtomValue(tripConversationAtom(props.tripId));
  if (!AsyncResult.isSuccess(conversation) || !isOpen(conversation.value)) return null;
  const { closesAt } = conversation.value;
  return (
    <CounterpartCard
      tripId={props.tripId}
      userId={props.userId}
      role={props.role}
      detail={closesAt === ""
        ? "Left something behind? Message them."
        : `Left something behind? Message them until ${formatClock(closesAt)}.`}
    />
  );
};
