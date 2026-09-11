import type { ConversationId, UserId } from "@surge/domain/api/Primitives";
import {
  type Conversation,
  type Draft,
  mergeMessages,
  type Message,
  newestSeq,
  withReceipt,
} from "@surge/domain/chat/Chat";
import type { ServerMessage } from "@surge/domain/realtime/Wire";
import {
  Clock,
  Context,
  Duration,
  Effect,
  Layer,
  Option,
  PubSub,
  Ref,
  Result,
  Stream,
} from "effect";
import { AuthToken, type TokenUnavailable } from "./AuthToken.js";
import { Realtime } from "./Realtime.js";
import { SurgeApi, type SurgeApiClient } from "./SurgeApi.js";

/**
 * Chat, as streams.
 *
 * The gateway's socket carries doorbells only — `ChatChanged` names a
 * conversation and its newest seq — and the messages are read over REST. This
 * is the half that turns the two into one thing a screen can render: a live
 * view of a conversation that opens on its newest page, reads whatever a
 * doorbell or a reconnect says it missed, applies read receipts as they
 * arrive, and shows the caller's own messages the moment they are sent.
 *
 * Messages are numbered per conversation without gaps, so catching up is
 * always "everything after the newest seq I hold", and a doorbell that
 * arrives late, twice, out of order or not at all costs nothing: the next
 * read collects whatever it announced.
 *
 * Platform-free, like the rest of this package: it is `SurgeApi` and
 * `Realtime` put together, and both of those take their platform from the app.
 */

type ChatClient = SurgeApiClient["chat"];

/**
 * What reading a conversation can fail with: the API's own errors — reading
 * the conversation and its messages fail the same ways — or no session to
 * read it as.
 */
export type ReadError = Effect.Error<ReturnType<ChatClient["get"]>> | TokenUnavailable;

export type SendError = Effect.Error<ReturnType<ChatClient["send"]>>;
export type MarkReadError = Effect.Error<ReturnType<ChatClient["markRead"]>>;
export type TypingError = Effect.Error<ReturnType<ChatClient["typing"]>>;
export type HistoryError = Effect.Error<ReturnType<ChatClient["listMessages"]>>;

/** A message sent from here that the server has not stored yet. */
export interface PendingMessage {
  readonly draft: Draft;
  /**
   * Present once the send failed. Retrying with the same draft — the same
   * `clientMessageId` — is safe: the server stores one message per key.
   */
  readonly failure: Option.Option<SendError>;
}

/** One conversation, live. */
export interface ConversationView {
  /** Who is looking, so a screen can tell its own messages from the other side's. */
  readonly me: UserId;
  readonly conversation: Conversation;
  /** Oldest first and without gaps, from the page it opened on to the newest. */
  readonly messages: ReadonlyArray<Message>;
  /** Older messages exist before the first one here, read with `earlier`. */
  readonly hasEarlier: boolean;
  /** Sends from here not stored yet, in the order they were made. */
  readonly pending: ReadonlyArray<PendingMessage>;
}

/** A page of history, oldest first. */
export interface History {
  readonly messages: ReadonlyArray<Message>;
  readonly hasMore: boolean;
}

export interface ChatService {
  /** A conversation, live, for as long as the stream is read. */
  readonly live: (conversationId: ConversationId) => Stream.Stream<ConversationView, ReadError>;
  /** Who else is typing there now. Each lapses `TYPING_LAPSES_AFTER` after the last hint. */
  readonly typing: (conversationId: ConversationId) => Stream.Stream<ReadonlyArray<UserId>>;
  /** The page of history before `beforeSeq`, for scrolling back. */
  readonly earlier: (
    conversationId: ConversationId,
    beforeSeq: number,
  ) => Effect.Effect<History, HistoryError>;
  /**
   * Send a message. It shows in every live view of the conversation at once,
   * as pending, and becomes the stored message when the server answers.
   */
  readonly send: (
    conversationId: ConversationId,
    draft: Draft,
  ) => Effect.Effect<Message, SendError>;
  /** Move the caller's read marker to `seq`. Returns where it is now, which may be further. */
  readonly markRead: (
    conversationId: ConversationId,
    seq: number,
  ) => Effect.Effect<number, MarkReadError>;
  /**
   * Say the caller is typing. Call it on every keystroke: it reaches the
   * server at most once every `TYPING_EVERY`, and the server throttles again.
   */
  readonly announceTyping: (conversationId: ConversationId) => Effect.Effect<void, TypingError>;
}

/** How long a typing hint lasts without another. The server throttles to one every three seconds. */
export const TYPING_LAPSES_AFTER = Duration.seconds(5);

/** The least time between two typing hints from here, per conversation. */
export const TYPING_EVERY = Duration.seconds(3);

/** How much a conversation opens on. A pickup chat is rarely longer. */
const OPENING_PAGE = 50;

/** How much one catch-up read asks for; it reads again while there is more. */
const CATCH_UP_PAGE = 200;

/** How much one scroll back reads. */
const HISTORY_PAGE = 50;

/**
 * What happened here, told to every live view of a conversation — the socket
 * never tells the actor about their own actions.
 */
type Local =
  | { readonly _tag: "Sending"; readonly conversationId: ConversationId; readonly draft: Draft; }
  | { readonly _tag: "Sent"; readonly conversationId: ConversationId; readonly message: Message; }
  | {
    readonly _tag: "SendFailed";
    readonly conversationId: ConversationId;
    readonly clientMessageId: string;
    readonly error: SendError;
  }
  | { readonly _tag: "ReadHere"; readonly conversationId: ConversationId; readonly seq: number; };

/** Everything that moves a live view. */
type Event =
  | Local
  | { readonly _tag: "Changed"; readonly lastSeq: number; }
  | { readonly _tag: "Receipt"; readonly userId: UserId; readonly seq: number; }
  | { readonly _tag: "Connected"; };

/** What a frame from the socket means for one conversation, if anything. */
const eventOf =
  (conversationId: ConversationId) =>
  (message: ServerMessage): Result.Result<Event, ServerMessage> => {
    if (message._tag === "ChatChanged" && message.chat.conversationId === conversationId) {
      return Result.succeed({ _tag: "Changed", lastSeq: message.chat.lastSeq });
    }
    if (message._tag === "ChatRead" && message.chatRead.conversationId === conversationId) {
      return Result.succeed({
        _tag: "Receipt",
        userId: message.chatRead.userId,
        seq: message.chatRead.seq,
      });
    }
    return Result.fail(message);
  };

/** Drops pending sends the server has stored — found by their key among the caller's own messages. */
const settle = (view: ConversationView): ConversationView => {
  const stored = new Set(
    view.messages.filter((message) => message.senderId === view.me).map((message) =>
      message.clientMessageId
    ),
  );
  return view.pending.some((pending) => stored.has(pending.draft.clientMessageId))
    ? {
      ...view,
      pending: view.pending.filter((pending) => !stored.has(pending.draft.clientMessageId)),
    }
    : view;
};

/** The view with messages merged in and the conversation's newest seq kept up with them. */
const withMessages = (view: ConversationView, incoming: ReadonlyArray<Message>): ConversationView =>
  settle({
    ...view,
    messages: mergeMessages(view.messages, incoming),
    conversation: {
      ...view.conversation,
      lastSeq: Math.max(view.conversation.lastSeq, newestSeq(incoming)),
    },
  });

const durationMillis = Duration.toMillis;

export class Chat extends Context.Service<Chat, ChatService>()("Chat") {
  static layer: Layer.Layer<Chat, never, SurgeApi | Realtime | AuthToken> = Layer.effect(Chat)(
    Effect.gen(function*() {
      const api = yield* SurgeApi;
      const realtime = yield* Realtime;
      const auth = yield* AuthToken;

      const local = yield* PubSub.unbounded<Local>();
      const typedAt = yield* Ref.make<ReadonlyMap<ConversationId, number>>(new Map());

      /** Everything after `afterSeq`, reading again while the server says there is more. */
      const readAfter = (
        conversationId: ConversationId,
        afterSeq: number,
        read: ReadonlyArray<Message>,
      ): Effect.Effect<ReadonlyArray<Message>, HistoryError> =>
        api.chat.listMessages({
          params: { conversationId },
          query: { afterSeq, pageSize: CATCH_UP_PAGE },
        }).pipe(
          Effect.flatMap(({ hasMore, messages }) =>
            hasMore && messages.length > 0
              ? readAfter(conversationId, newestSeq(messages), [...read, ...messages])
              : Effect.succeed([...read, ...messages])
          ),
        );

      const open = Effect.fnUntraced(function*(conversationId: ConversationId) {
        const [{ conversation }, page, identity] = yield* Effect.all([
          api.chat.get({ params: { conversationId } }),
          api.chat.listMessages({ params: { conversationId }, query: { pageSize: OPENING_PAGE } }),
          auth.identity,
        ], { concurrency: "unbounded" });
        const view: ConversationView = {
          me: identity.userId,
          conversation,
          messages: page.messages,
          hasEarlier: page.hasMore,
          pending: [],
        };
        return view;
      });

      /**
       * Read the conversation again, and the messages after the newest held
       * when there may be some. The conversation is read every time, because a
       * doorbell is also how a claim, a resolve or a closing time arrives.
       */
      const refresh = Effect.fnUntraced(function*(view: ConversationView, catchUp: boolean) {
        const conversationId = view.conversation.id;
        const [{ conversation }, newer] = yield* Effect.all([
          api.chat.get({ params: { conversationId } }),
          catchUp ? readAfter(conversationId, newestSeq(view.messages), []) : Effect.succeed([]),
        ], { concurrency: "unbounded" });
        return withMessages({ ...view, conversation }, newer);
      });

      const step = (
        view: ConversationView,
        event: Event,
      ): Effect.Effect<ConversationView, ReadError> => {
        switch (event._tag) {
          case "Changed":
            return refresh(view, event.lastSeq > newestSeq(view.messages));
          case "Connected":
            // The gateway keeps no replay: whatever was pushed while the
            // socket was down is gone, so a connection is a reason to look.
            return refresh(view, true);
          case "Receipt": {
            const conversation = withReceipt(view.conversation, event.userId, event.seq);
            // A late receipt moves nothing, and must not look like a change.
            return Effect.succeed(
              conversation === view.conversation ? view : { ...view, conversation },
            );
          }
          case "ReadHere": {
            const conversation = withReceipt(view.conversation, view.me, event.seq);
            return Effect.succeed(
              conversation === view.conversation ? view : {
                ...view,
                conversation: {
                  ...conversation,
                  unread: Math.max(0, conversation.lastSeq - event.seq),
                },
              },
            );
          }
          case "Sending":
            return Effect.succeed({
              ...view,
              pending: [
                ...view.pending.filter((pending) =>
                  pending.draft.clientMessageId !== event.draft.clientMessageId
                ),
                { draft: event.draft, failure: Option.none() },
              ],
            });
          case "SendFailed":
            return Effect.succeed({
              ...view,
              pending: view.pending.map((pending) =>
                pending.draft.clientMessageId === event.clientMessageId
                  ? { ...pending, failure: Option.some(event.error) }
                  : pending
              ),
            });
          case "Sent": {
            // The sender has read their own message, as the server records.
            const next = withMessages(view, [event.message]);
            return Effect.succeed({
              ...next,
              conversation: withReceipt(
                next.conversation,
                event.message.senderId,
                event.message.seq,
              ),
            });
          }
        }
      };

      const live = (conversationId: ConversationId): Stream.Stream<ConversationView, ReadError> =>
        Stream.unwrap(
          Effect.gen(function*() {
            // Subscribed before the first read, so a doorbell rung while the
            // page loads is held rather than lost.
            const frames = yield* realtime.subscribe;
            const here = yield* PubSub.subscribe(local);
            const view = yield* open(conversationId);

            const events: Stream.Stream<Event> = Stream.mergeAll([
              Stream.filterMap(frames, eventOf(conversationId)),
              Stream.fromSubscription(here).pipe(
                Stream.filter((event) => event.conversationId === conversationId),
              ),
              // Every Connected, the one current on subscribe included: a
              // socket that came up while the page was loading may have
              // missed a doorbell, and one extra read is the cheap answer.
              realtime.status.pipe(
                Stream.changes,
                Stream.filter((status) => status === "Connected"),
                Stream.map((): Event => ({ _tag: "Connected" })),
              ),
            ], { concurrency: "unbounded" });

            return Stream.concat(
              Stream.make(view),
              events.pipe(
                Stream.mapAccumEffect(
                  () => view,
                  (held, event) =>
                    Effect.map(
                      step(held, event),
                      (next) => [next, next === held ? [] : [next]] as const,
                    ),
                ),
              ),
            );
          }),
        );

      const typing = (conversationId: ConversationId): Stream.Stream<ReadonlyArray<UserId>> => {
        const hints = Stream.filterMap(
          realtime.chatTyping,
          (hint) =>
            hint.conversationId === conversationId
              ? Result.succeed(Option.some(hint.userId))
              : Result.fail(hint),
        );
        // A tick, so a hint lapses on time rather than when the next one arrives.
        const ticks = Stream.tick("1 second").pipe(Stream.map(() => Option.none<UserId>()));
        const lapse = durationMillis(TYPING_LAPSES_AFTER);

        return Stream.merge(hints, ticks).pipe(
          Stream.mapAccumEffect(
            (): ReadonlyMap<UserId, number> => new Map(),
            (seen, hint) =>
              // The time it arrived here, not the server's: two clocks never
              // agree to the second, and a hint only has to last a few.
              Effect.map(Clock.currentTimeMillis, (now) => {
                const fresh = new Map([...seen].filter(([, at]) => now - at < lapse));
                const next = Option.match(hint, {
                  onNone: () => fresh,
                  onSome: (userId) => new Map([...fresh, [userId, now]]),
                });
                return [next, [[...next.keys()].sort()]] as const;
              }),
          ),
          Stream.changesWith((a, b) => a.length === b.length && a.every((id, i) => id === b[i])),
        );
      };

      const send = Effect.fnUntraced(function*(conversationId: ConversationId, draft: Draft) {
        yield* PubSub.publish(local, { _tag: "Sending", conversationId, draft });
        const { message } = yield* api.chat.send({ params: { conversationId }, payload: draft })
          .pipe(
            Effect.tapError((error) =>
              PubSub.publish(local, {
                _tag: "SendFailed",
                conversationId,
                clientMessageId: draft.clientMessageId,
                error,
              })
            ),
          );
        yield* PubSub.publish(local, { _tag: "Sent", conversationId, message });
        return message;
      });

      const markRead = Effect.fnUntraced(function*(conversationId: ConversationId, seq: number) {
        const { lastReadSeq } = yield* api.chat.markRead({
          params: { conversationId },
          payload: { seq },
        });
        yield* PubSub.publish(local, { _tag: "ReadHere", conversationId, seq: lastReadSeq });
        return lastReadSeq;
      });

      const every = durationMillis(TYPING_EVERY);
      const announceTyping = Effect.fnUntraced(function*(conversationId: ConversationId) {
        const now = yield* Clock.currentTimeMillis;
        const due = yield* Ref.modify(
          typedAt,
          (last): readonly [boolean, ReadonlyMap<ConversationId, number>] => {
            const at = last.get(conversationId);
            return at !== undefined && now - at < every
              ? [false, last]
              : [true, new Map([...last, [conversationId, now]])];
          },
        );
        if (due) yield* api.chat.typing({ params: { conversationId }, payload: {} });
      });

      return {
        live,
        typing,
        earlier: (conversationId, beforeSeq) =>
          api.chat.listMessages({
            params: { conversationId },
            query: { beforeSeq, pageSize: HISTORY_PAGE },
          }),
        send,
        markRead,
        announceTyping,
      };
    }),
  );
}
