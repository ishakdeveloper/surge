import { Chat } from "@surge/client/Chat";
import { Realtime } from "@surge/client/Realtime";
import { SurgeApi } from "@surge/client/SurgeApi";
import type { ConversationId, TripId } from "@surge/domain/api/Primitives";
import type { Draft } from "@surge/domain/chat/Chat";
import { Data, Effect, Stream } from "effect";
import { Atom, Reactivity } from "effect/unstable/reactivity";
import { Keys } from "./reactivity-keys.js";
import { runtime } from "./runtime.js";

/**
 * Chat, as atoms.
 *
 * Two kinds of state, kept apart. Lists — the caller's conversations and
 * their unread counts, support's queue — are queries under `Keys.chat`,
 * invalidated by `chatPushesAtom` whenever a doorbell rings. A conversation
 * that is open on screen is a stream instead: `conversationAtom` is
 * `Chat.live`, which reads what each doorbell announced rather than
 * refetching the whole conversation.
 */

// --- lists -------------------------------------------------------------------

/** The caller's conversations, newest first, with what each has unread. */
export const conversationsAtom = Atom.withReactivity([Keys.chat])(
  runtime.atom(
    Effect.gen(function*() {
      const api = yield* SurgeApi;
      const { conversations } = yield* api.chat.list({ query: {} });
      return conversations;
    }),
  ),
);

/**
 * Support's queue: every open support conversation, whoever holds it. Support
 * only; anyone else asking for it gets their own support conversations.
 */
export const supportQueueAtom = Atom.withReactivity([Keys.chat])(
  runtime.atom(
    Effect.gen(function*() {
      const api = yield* SurgeApi;
      const { conversations } = yield* api.chat.list({
        query: { kind: "CONVERSATION_KIND_SUPPORT", status: "CONVERSATION_STATUS_OPEN" },
      });
      return conversations;
    }),
  ),
);

/**
 * The conversation about a trip.
 *
 * A failed precondition until a driver accepts, which is the honest state
 * rather than an empty one. Keyed on trips too, so the push that says the
 * trip was accepted is also what makes this read again and find it.
 */
export const tripConversationAtom = Atom.family((tripId: TripId) =>
  Atom.withReactivity([Keys.chat, Keys.trips])(
    runtime.atom(
      Effect.gen(function*() {
        const api = yield* SurgeApi;
        const { conversation } = yield* api.chat.getForTrip({ params: { tripId } });
        return conversation;
      }),
    ),
  )
);

// --- one conversation ----------------------------------------------------------

/**
 * A conversation, live, for as long as it is on screen: its newest page and
 * everything after, receipts, and the caller's own sends shown at once.
 */
export const conversationAtom = Atom.family((conversationId: ConversationId) =>
  runtime.atom(Stream.unwrap(Effect.map(Chat, (chat) => chat.live(conversationId))))
);

/** Who else is typing in a conversation. Each hint lapses on its own. */
export const typingAtom = Atom.family((conversationId: ConversationId) =>
  runtime.atom(Stream.unwrap(Effect.map(Chat, (chat) => chat.typing(conversationId))))
);

/** A page of history, before a seq. */
export class EarlierKey extends Data.Class<{
  readonly conversationId: ConversationId;
  readonly beforeSeq: number;
}> {
  static make(args: { readonly conversationId: ConversationId; readonly beforeSeq: number; }) {
    return new EarlierKey(args);
  }
}

/**
 * History for scrolling back: the page before a seq, oldest first. The screen
 * asks for the page before the first message it shows, and again before that.
 */
export const earlierMessagesAtom = Atom.family((key: EarlierKey) =>
  runtime.atom(
    Effect.gen(function*() {
      const chat = yield* Chat;
      return yield* chat.earlier(key.conversationId, key.beforeSeq);
    }),
  )
);

// --- actions -------------------------------------------------------------------

/**
 * Send a message. The caller makes the draft's `clientMessageId` and keeps it
 * across retries, as with booking: a key made in here would be a second
 * message on every retry.
 */
export const sendMessage = runtime.fn(
  Effect.fnUntraced(function*(send: {
    readonly conversationId: ConversationId;
    readonly draft: Draft;
  }) {
    const chat = yield* Chat;
    return yield* chat.send(send.conversationId, send.draft);
  }),
  // Concurrent: a second message sent before the first is answered must not
  // interrupt it. An interrupted send never reports failing, so it would sit
  // in the thread as "Sending…" for good.
  { reactivityKeys: [Keys.chat], concurrent: true },
);

/** Mark a conversation read up to a seq — the newest one on screen. */
export const markRead = runtime.fn(
  Effect.fnUntraced(
    function*(read: { readonly conversationId: ConversationId; readonly seq: number; }) {
      const chat = yield* Chat;
      return yield* chat.markRead(read.conversationId, read.seq);
    },
  ),
  { reactivityKeys: [Keys.chat] },
);

/** Call on every keystroke; it reaches the server at most once every few seconds. */
export const announceTyping = runtime.fn(
  Effect.fnUntraced(function*(conversationId: ConversationId) {
    const chat = yield* Chat;
    yield* chat.announceTyping(conversationId);
  }),
);

/**
 * Ask support for help, optionally about one of the caller's trips, with the
 * first message. The caller holds the idempotency key across retries.
 */
export const contactSupport = runtime.fn(
  Effect.fnUntraced(function*(request: {
    readonly tripId: TripId;
    readonly subject: string;
    readonly body: string;
    readonly idempotencyKey: string;
  }) {
    const api = yield* SurgeApi;
    const { conversation } = yield* api.support.create({ payload: request });
    return conversation;
  }),
  { reactivityKeys: [Keys.chat] },
);

/** Support takes a conversation on. Replying to an unclaimed one claims it too. */
export const claimConversation = runtime.fn(
  Effect.fnUntraced(function*(conversationId: ConversationId) {
    const api = yield* SurgeApi;
    const { conversation } = yield* api.support.claim({ params: { conversationId }, payload: {} });
    return conversation;
  }),
  { reactivityKeys: [Keys.chat] },
);

/** End a support conversation, by support or by whoever opened it. */
export const resolveConversation = runtime.fn(
  Effect.fnUntraced(function*(conversationId: ConversationId) {
    const api = yield* SurgeApi;
    const { conversation } = yield* api.support.resolve({
      params: { conversationId },
      payload: {},
    });
    return conversation;
  }),
  { reactivityKeys: [Keys.chat] },
);

/**
 * Let this device be notified of messages left unread. The token comes from
 * Expo's notifications module on the phone, which is platform code and stays
 * in the app; this only hands it over.
 */
export const registerPushToken = runtime.fn(
  Effect.fnUntraced(function*(device: {
    readonly token: string;
    readonly platform: "PUSH_PLATFORM_IOS" | "PUSH_PLATFORM_ANDROID";
  }) {
    const api = yield* SurgeApi;
    yield* api.devices.registerPushToken({ payload: device });
  }),
);

/** Stop notifying this device, on sign-out. */
export const unregisterPushToken = runtime.fn(
  Effect.fnUntraced(function*(token: string) {
    const api = yield* SurgeApi;
    yield* api.devices.unregisterPushToken({ payload: { token } });
  }),
);

// --- pushes --------------------------------------------------------------------

/**
 * Doorbells, turned into invalidation of the lists. Mounted by every screen
 * that shows conversations, for the two reasons `tripPushesAtom` gives: a
 * `ChatChanged`, and a reconnect, after which a doorbell rung while the socket
 * was down is gone.
 */
export const chatPushesAtom = runtime.atom(
  Stream.unwrap(
    Effect.map(Realtime, (realtime) =>
      Stream.merge(
        realtime.chatChanged,
        realtime.status.pipe(
          Stream.changes,
          Stream.filter((status) => status === "Connected"),
          Stream.drop(1),
        ),
      ).pipe(Stream.mapEffect(() => Reactivity.invalidate([Keys.chat])))),
  ),
);
