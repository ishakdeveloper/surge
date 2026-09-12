import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { announceTyping, conversationAtom, sendMessage } from "@surge/common/atom/chat-atoms";
import { draftAtom } from "@surge/common/atom/chat-drafts";
import { closedNote, counterpartName } from "@surge/common/chat/thread";
import type { ConversationId, UserId } from "@surge/domain/api/Primitives";
import { type Conversation, isOpen } from "@surge/domain/chat/Chat";
import { AsyncResult } from "effect/unstable/reactivity";
import { ArrowUp } from "lucide-react";
import * as React from "react";

/** The server's limit on a message's text. */
const MAX_LENGTH = 1000;

/** Past this, the composer says how much room is left. */
const COUNT_FROM = 900;

// A click handler needs the key now, not an Effect to run for it; it only has
// to be unique, which this is.
// oxlint-disable-next-line effecttsgo/crypto-random-uuid
const newKey = () => crypto.randomUUID();

/**
 * Where a message is written: the conversation's quick replies as one-tap
 * pills while nothing is typed, a grey field that lifts white as it takes
 * focus, and a yellow send button — the next step — that stays grey until
 * there is something to send. Enter sends; Shift+Enter starts a new line.
 *
 * A closed or resolved conversation shows why instead, since it reads but no
 * longer takes messages.
 */
export const ChatComposer = (props: {
  readonly conversationId: ConversationId;
  readonly staff?: boolean;
  readonly autoFocus?: boolean;
}) => {
  const view = useAtomValue(conversationAtom(props.conversationId));
  // The thread beside this shows the conversation loading or failing to; a
  // composer for a conversation that is not here has nowhere to send.
  if (!AsyncResult.isSuccess(view)) return null;
  const { conversation, me } = view.value;
  const staff = props.staff === true;

  if (!isOpen(conversation)) {
    return (
      <p className="rounded-2xl bg-tile px-4 py-3 text-sm text-pretty text-muted-foreground">
        {closedNote(conversation, staff)}
      </p>
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
  const id = React.useId();
  const countId = `${id}-count`;
  const text = draft.text;
  const empty = text.trim() === "";

  const sendText = () => {
    const body = text.trim();
    if (body === "") return;
    send({
      conversationId: conversation.id,
      draft: { body, quickReply: "", clientMessageId: newKey() },
    });
    setDraft({ text: "" });
  };

  return (
    <div className="flex flex-col gap-2">
      {empty && conversation.quickReplies.length > 0 && (
        <div
          role="group"
          aria-label="Quick replies"
          className="-mx-1 flex gap-2 overflow-x-auto px-1 pb-0.5 [scrollbar-width:none]"
        >
          {conversation.quickReplies.map((reply) => (
            <button
              key={reply.code}
              type="button"
              onClick={() => {
                send({
                  conversationId: conversation.id,
                  draft: { body: "", quickReply: reply.code, clientMessageId: newKey() },
                });
              }}
              className="shrink-0 cursor-pointer rounded-full bg-tile px-3.5 py-2 text-sm font-semibold whitespace-nowrap transition-[background-color,transform] duration-150 ease-out hover:bg-tile-hover active:scale-95"
            >
              {reply.text}
            </button>
          ))}
        </div>
      )}
      <form
        className="flex items-end gap-2"
        onSubmit={(event) => {
          event.preventDefault();
          sendText();
        }}
      >
        <label htmlFor={id} className="sr-only">
          Message {counterpartName(conversation, props.me, props.staff).toLowerCase()}
        </label>
        <textarea
          id={id}
          rows={1}
          // Focused only when the reader asked for the conversation, never on
          // a page that merely shows one.
          autoFocus={props.autoFocus}
          maxLength={MAX_LENGTH}
          value={text}
          placeholder="Message"
          aria-describedby={text.length >= COUNT_FROM ? countId : undefined}
          onChange={(event) => {
            setDraft({ text: event.target.value });
            if (event.target.value !== "") typing(conversation.id);
          }}
          onKeyDown={(event) => {
            if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
              event.preventDefault();
              sendText();
            }
          }}
          className="field-sizing-content max-h-32 min-h-11 flex-1 resize-none rounded-[22px] bg-tile px-4 py-2.5 text-[15px] leading-snug outline-none transition-[background-color,box-shadow] duration-150 ease-out placeholder:text-muted-foreground focus:bg-card focus:shadow-[inset_0_0_0_2px_var(--foreground)]"
        />
        <button
          type="submit"
          aria-label="Send"
          disabled={empty}
          className="grid size-11 shrink-0 cursor-pointer place-items-center rounded-full bg-primary text-primary-foreground transition-[background-color,color,transform] duration-150 ease-out hover:bg-[#f5c900] active:scale-95 disabled:cursor-not-allowed disabled:bg-tile disabled:text-muted-foreground disabled:active:scale-100"
        >
          <ArrowUp aria-hidden className="size-5" strokeWidth={2.5} />
        </button>
      </form>
      {text.length >= COUNT_FROM && (
        <p id={countId} className="px-4 text-right text-xs text-muted-foreground tabular-nums">
          {MAX_LENGTH - text.length} characters left
        </p>
      )}
    </div>
  );
};
