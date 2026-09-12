import { QueryError } from "@/components/app/query-error.js";
import { ChatComposer } from "@/components/chat/chat-composer.js";
import { ChatThread } from "@/components/chat/chat-thread.js";
import { CounterpartCard } from "@/components/chat/counterpart-card.js";
import { PersonAvatar, usePersonName } from "@/components/profile/avatar.js";
import { useAtomValue } from "@effect/atom-react";
import { tripConversationAtom, typingAtom } from "@surge/common/atom/chat-atoms";
import type { ConversationId, TripId, UserId } from "@surge/domain/api/Primitives";
import { AsyncResult } from "effect/unstable/reactivity";
import { ArrowLeft } from "lucide-react";
import * as React from "react";

/**
 * The conversation about a trip, for its rider and its driver.
 *
 * It exists from the moment a driver accepts; before that the API answers
 * with a failed precondition. So these render only once there is a driver,
 * and their parent decides when that is.
 */

/**
 * The conversation itself, in place of the trip's cards: a way back, the
 * other person's face and first name with what the trip is doing, then the
 * thread. Escape goes back too.
 */
export const TripChatPanel = (props: {
  readonly tripId: TripId;
  readonly counterpartId: UserId;
  /** Who they are until they give a name: "Your driver", "Your rider". */
  readonly role: string;
  /** What the trip is doing, shown under the name while nobody is typing. */
  readonly subtitle: string;
  readonly onClose: () => void;
}) => {
  const result = useAtomValue(tripConversationAtom(props.tripId));
  const name = usePersonName(props.counterpartId, props.role);
  const headingId = React.useId();
  const onClose = React.useRef(props.onClose);
  onClose.current = props.onClose;

  React.useEffect(() => {
    const escape = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose.current();
    };
    document.addEventListener("keydown", escape);
    return () => {
      document.removeEventListener("keydown", escape);
    };
  }, []);

  return (
    <section
      aria-labelledby={headingId}
      className="flex flex-col gap-3 motion-safe:animate-rise-in"
    >
      <header className="flex items-center gap-3 px-1">
        <button
          type="button"
          onClick={props.onClose}
          aria-label="Back to your trip"
          className="grid size-9 shrink-0 cursor-pointer place-items-center rounded-full bg-tile transition-colors duration-150 hover:bg-tile-hover"
        >
          <ArrowLeft aria-hidden className="size-4" />
        </button>
        <PersonAvatar userId={props.counterpartId} size="sm" />
        <div className="flex min-w-0 flex-1 flex-col">
          <h2 id={headingId} className="truncate text-lg leading-tight font-bold">{name}</h2>
          {AsyncResult.isSuccess(result)
            ? <Subtitle conversationId={result.value.id} fallback={props.subtitle} />
            : <p className="truncate text-[13px] text-muted-foreground">{props.subtitle}</p>}
        </div>
      </header>
      {AsyncResult.isInitial(result)
        ? (
          <p className="px-1 py-8 text-center text-sm text-muted-foreground">
            Opening the conversation…
          </p>
        )
        : AsyncResult.isFailure(result)
        ? <QueryError result={result} subject="this conversation" />
        : <ChatThread conversationId={result.value.id} />}
    </section>
  );
};

const Subtitle = (
  props: { readonly conversationId: ConversationId; readonly fallback: string; },
) => {
  const typing = useAtomValue(typingAtom(props.conversationId));
  return (
    <p className="truncate text-[13px] text-muted-foreground">
      {AsyncResult.isSuccess(typing) && typing.value.length > 0 ? "Typing…" : props.fallback}
    </p>
  );
};

/** The composer for a trip's conversation, once there is one to write in. */
export const TripChatComposer = (props: { readonly tripId: TripId; }) => {
  const result = useAtomValue(tripConversationAtom(props.tripId));
  return AsyncResult.isSuccess(result)
    ? <ChatComposer conversationId={result.value.id} autoFocus />
    : null;
};

/**
 * The other person and the conversation in one place, for a page with no
 * pinned footer: their card, whose Message button opens the thread and the
 * composer where the card stood, and back closes them.
 */
export const TripChat = (props: {
  readonly tripId: TripId;
  readonly counterpartId: UserId;
  readonly role: string;
  readonly subtitle: string;
}) => {
  const [open, setOpen] = React.useState(false);
  if (!open) {
    return (
      <CounterpartCard
        tripId={props.tripId}
        userId={props.counterpartId}
        role={props.role}
        detail={props.subtitle}
        onMessage={() => {
          setOpen(true);
        }}
      />
    );
  }
  return (
    <div className="flex flex-col gap-3">
      <TripChatPanel
        tripId={props.tripId}
        counterpartId={props.counterpartId}
        role={props.role}
        subtitle={props.subtitle}
        onClose={() => {
          setOpen(false);
        }}
      />
      <TripChatComposer tripId={props.tripId} />
    </div>
  );
};
