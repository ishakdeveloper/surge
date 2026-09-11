import { AuthToken } from "@/AuthToken.js";
import { Chat, type ConversationView } from "@/Chat.js";
import { Realtime } from "@/Realtime.js";
import { SurgeApi } from "@/SurgeApi.js";
import { describe, expect, it } from "@effect/vitest";
import { ConversationId, UserId } from "@surge/domain/api/Primitives";
import type { Conversation, Message } from "@surge/domain/chat/Chat";
import { readersOf } from "@surge/domain/chat/Chat";
import { Identity } from "@surge/domain/iam/Identity";
import { Effect, Fiber, Layer, Option, Ref, Stream } from "effect";
import { TestClock } from "effect/testing";
import { Socket } from "effect/unstable/socket";
import {
  fakeHttp,
  type FakeHttpRequest,
  type FakeHttpResponse,
  notFound,
} from "./support/fake-http.js";
import { FakeWebSocket, settle } from "./support/fake-websocket.js";

/**
 * A conversation, live, against a fake gateway: its socket in-process, and its
 * REST answered from a conversation this file holds.
 *
 * What is worth testing is the sync, which no compiler checks: that a doorbell
 * brings what it announced and a reconnect brings what the socket missed,
 * that receipts only move forward, and that the caller's own messages show at
 * once and survive a failed send.
 */

const conversationId = ConversationId.make("c1");
const rider = UserId.make("rider-1");
const driver = UserId.make("drv-1");

const WELCOME = `{"_tag":"ServerWelcome"}`;
const changed = (lastSeq: number) =>
  JSON.stringify({
    _tag: "ChatChanged",
    chat: { conversationId, kind: "trip", tripId: "trip-1", lastSeq, atMs: 1 },
  });
const receipt = (seq: number) =>
  JSON.stringify({ _tag: "ChatRead", chatRead: { conversationId, userId: driver, seq, atMs: 1 } });
const typingHint = (userId: string, id: string = conversationId) =>
  JSON.stringify({ _tag: "ChatTyping", chatTyping: { conversationId: id, userId, atMs: 1 } });

/** The server's side: one trip conversation, and whatever has been sent in it. */
const server = () => {
  const messages: Array<Message> = [];
  const state = { failSends: 0 };

  const store = (sender: UserId, body: string, clientMessageId: string): Message => {
    const stored = messages.find((m) =>
      m.senderId === sender && m.clientMessageId === clientMessageId
    );
    if (stored !== undefined) return stored;
    const message: Message = {
      id: `m-${messages.length + 1}` as Message["id"],
      conversationId,
      seq: messages.length + 1,
      senderId: sender,
      senderRole: sender === rider ? "PARTICIPANT_ROLE_RIDER" : "PARTICIPANT_ROLE_DRIVER",
      quickReply: "",
      body,
      clientMessageId,
      createdAt: "2026-09-11T12:00:00Z",
    };
    messages.push(message);
    return message;
  };

  const conversation = (): Conversation => ({
    id: conversationId,
    kind: "CONVERSATION_KIND_TRIP",
    tripId: "trip-1" as Conversation["tripId"],
    status: "CONVERSATION_STATUS_OPEN",
    subject: "",
    requesterId: rider,
    assigneeId: UserId.make(""),
    lastSeq: messages.length,
    unread: 0,
    closesAt: "",
    participants: [
      { userId: rider, role: "PARTICIPANT_ROLE_RIDER", lastReadSeq: 0 },
      { userId: driver, role: "PARTICIPANT_ROLE_DRIVER", lastReadSeq: 0 },
    ],
    quickReplies: [],
    createdAt: "2026-09-11T12:00:00Z",
    updatedAt: "2026-09-11T12:00:00Z",
  });

  const answer = (request: FakeHttpRequest): FakeHttpResponse => {
    const base = `/v1/conversations/${conversationId}`;
    if (request.method === "GET" && request.path === base) {
      return { body: { conversation: conversation() } };
    }
    if (request.method === "GET" && request.path === `${base}/messages`) {
      const after = Number(request.query.get("afterSeq") ?? 0);
      return { body: { messages: messages.filter((m) => m.seq > after), hasMore: false } };
    }
    if (request.method === "POST" && request.path === `${base}/messages`) {
      if (state.failSends > 0) {
        state.failSends--;
        // Not a 5xx: the client retries those itself, and this is about what
        // a screen sees when a send does fail.
        return {
          status: 409,
          body: { error: { code: "failed_precondition", message: "try again" } },
        };
      }
      const draft = request.body as { body: string; clientMessageId: string; };
      return { body: { message: store(rider, draft.body, draft.clientMessageId) } };
    }
    if (request.method === "POST" && request.path === `${base}/read`) {
      return { body: { lastReadSeq: (request.body as { seq: number; }).seq } };
    }
    if (request.method === "POST" && request.path === `${base}/typing`) return { body: {} };
    return notFound(request);
  };

  return { messages, state, store, answer };
};

const harness = () => {
  const sockets: Array<FakeWebSocket> = [];
  const backend = server();
  const http = fakeHttp(backend.answer);

  const layer = Chat.layer.pipe(
    Layer.provideMerge(Layer.mergeAll(SurgeApi.layer, Realtime.layer)),
    Layer.provideMerge(Layer.mergeAll(
      http.layer,
      Layer.succeed(Socket.WebSocketConstructor)((url) => {
        const socket = new FakeWebSocket(url);
        sockets.push(socket);
        // oxlint-disable-next-line typescript/no-unsafe-type-assertion
        return socket as unknown as globalThis.WebSocket;
      }),
      Layer.succeed(AuthToken)({
        get: Effect.succeed("a-token-nothing-here-verifies"),
        identity: Effect.succeed(
          new Identity({
            userId: rider,
            email: "rider@surge.test",
            emailVerified: true,
            role: "rider",
          }),
        ),
        invalidate: Effect.void,
      }),
    )),
  );

  /** The socket the client is on now, once its read loop is attached. */
  const socket = eventually(
    Effect.sync(() => sockets.at(-1)),
    (latest) => latest !== undefined && latest.listening,
  ).pipe(Effect.map((latest) => latest!));

  return { backend, http, layer, sockets, socket };
};

/**
 * Polls until a condition holds. HTTP responses are real promises, so these
 * tests run on the live clock and wait for them rather than yielding.
 */
const eventually = Effect.fnUntraced(
  function*<A>(read: Effect.Effect<A>, holds: (value: A) => boolean) {
    for (let attempt = 0; attempt < 300; attempt++) {
      const value = yield* read;
      if (holds(value)) return value;
      yield* Effect.sleep("10 millis");
    }
    return yield* Effect.die("the condition never held");
  },
);

/** Runs a conversation's live view in the background, keeping the latest. */
const watch = Effect.fnUntraced(function*() {
  const chat = yield* Chat;
  const latest = yield* Ref.make(Option.none<ConversationView>());
  yield* Effect.forkScoped(
    Stream.runForEach(chat.live(conversationId), (view) => Ref.set(latest, Option.some(view))),
  );
  const until = (holds: (view: ConversationView) => boolean) =>
    eventually(Ref.get(latest), (view) => Option.isSome(view) && holds(view.value)).pipe(
      Effect.map(Option.getOrThrow),
    );
  return { until };
});

const seqs = (view: ConversationView) => view.messages.map((message) => message.seq);

describe("a live conversation", () => {
  it.live("opens on the newest page, and a doorbell brings what it announced", () =>
    Effect.gen(function*() {
      const { backend, http, layer, socket } = harness();
      backend.store(rider, "At the side entrance", "k1");
      backend.store(driver, "Two minutes", "k2");

      yield* Effect.gen(function*() {
        const view = yield* watch();
        const opened = yield* view.until((v) => v.messages.length === 2);
        expect(opened.me).toBe(rider);

        const live = yield* socket;
        live.deliver(WELCOME);
        backend.store(driver, "I'm here", "k3");
        live.deliver(changed(3));

        const caughtUp = yield* view.until((v) => seqs(v).includes(3));
        expect(seqs(caughtUp)).toEqual([1, 2, 3]);
        expect(caughtUp.conversation.lastSeq).toBe(3);
        // Everything after the newest seq held, not the whole conversation again.
        expect(http.requests.some((r) => r.query.get("afterSeq") === "2")).toBe(true);
      }).pipe(Effect.scoped, Effect.provide(layer));
    }));

  it.live("reads what the socket missed once it reconnects", () =>
    Effect.gen(function*() {
      const { backend, layer, socket, sockets } = harness();
      backend.store(rider, "Hello?", "k1");

      yield* Effect.gen(function*() {
        const view = yield* watch();
        yield* view.until((v) => v.messages.length === 1);
        const first = yield* socket;
        first.deliver(WELCOME);

        // Stored while the socket is down: its doorbell is gone for good.
        first.drop(1006, "network");
        backend.store(driver, "Sorry, here now", "k2");

        const second = yield* eventually(Effect.sync(() => sockets.at(-1)), (s) =>
          s !== first && s?.listening === true);
        second!.deliver(WELCOME);

        const caughtUp = yield* view.until((v) =>
          seqs(v).includes(2)
        );
        expect(seqs(caughtUp)).toEqual([1, 2]);
      }).pipe(Effect.scoped, Effect.provide(layer));
    }), { timeout: 10_000 });

  it.live("moves the other side's read marker forward, and never back", () =>
    Effect.gen(function*() {
      const { backend, layer, socket } = harness();
      backend.store(rider, "Blue coat", "k1");
      backend.store(rider, "By the flower stall", "k2");

      yield* Effect.gen(function*() {
        const view = yield* watch();
        yield* view.until((v) => v.messages.length === 2);
        const live = yield* socket;

        live.deliver(receipt(2));
        const seen = yield* view.until((v) => v.conversation.participants[1]?.lastReadSeq === 2);
        expect(readersOf(seen.conversation, seen.messages[1]!).map((p) => p.userId)).toEqual([
          driver,
        ]);

        // A receipt that arrives late says nothing new.
        live.deliver(receipt(1));
        yield* Effect.sleep("100 millis");
        const after = yield* view.until(() => true);
        expect(after.conversation.participants[1]?.lastReadSeq).toBe(2);
        expect(after).toBe(seen);
      }).pipe(Effect.scoped, Effect.provide(layer));
    }));

  it.live("shows a send at once, keeps a failed one, and settles its retry", () =>
    Effect.gen(function*() {
      const { backend, layer } = harness();
      backend.store(driver, "Where are you?", "k1");
      backend.state.failSends = 1;

      yield* Effect.gen(function*() {
        const chat = yield* Chat;
        const view = yield* watch();
        yield* view.until((v) => v.messages.length === 1);

        const draft = { body: "Coming out now", quickReply: "", clientMessageId: "mine-1" };
        const failed = yield* Effect.flip(chat.send(conversationId, draft));
        // The gateway's own error body, decoded: a screen can say why.
        expect(failed).toMatchObject({ error: { code: "failed_precondition" } });
        const kept = yield* view.until((v) =>
          v.pending.length === 1 && Option.isSome(v.pending[0]!.failure)
        );
        expect(kept.pending[0]?.draft.body).toBe("Coming out now");

        // The same key again: one message, however many tries it took.
        const stored = yield* chat.send(conversationId, draft);
        const settled = yield* view.until((v) =>
          v.pending.length === 0 && seqs(v).includes(stored.seq)
        );
        expect(settled.messages.at(-1)?.clientMessageId).toBe("mine-1");
        // The sender has read their own message.
        expect(settled.conversation.participants[0]?.lastReadSeq).toBe(stored.seq);
        expect(backend.messages).toHaveLength(2);
      }).pipe(Effect.scoped, Effect.provide(layer));
    }));

  it.live("clears the unread count the moment it is read here", () =>
    Effect.gen(function*() {
      const { backend, layer } = harness();
      backend.store(driver, "I'm here", "k1");
      backend.store(driver, "Grey Prius", "k2");

      yield* Effect.gen(function*() {
        const chat = yield* Chat;
        const view = yield* watch();
        yield* view.until((v) => v.messages.length === 2);

        expect(yield* chat.markRead(conversationId, 2)).toBe(2);
        const read = yield* view.until((v) => v.conversation.participants[0]?.lastReadSeq === 2);
        expect(read.conversation.unread).toBe(0);
      }).pipe(Effect.scoped, Effect.provide(layer));
    }));

  it.live("says it is typing at most once every few seconds, however fast the keys", () =>
    Effect.gen(function*() {
      const { http, layer } = harness();

      yield* Effect.gen(function*() {
        const chat = yield* Chat;
        for (let key = 0; key < 5; key++) yield* chat.announceTyping(conversationId);
        expect(http.requests.filter((r) => r.path.endsWith("/typing"))).toHaveLength(1);
      }).pipe(Effect.scoped, Effect.provide(layer));
    }));
});

describe("typing", () => {
  it.effect("shows who is typing in this conversation, and lets it lapse", () =>
    Effect.gen(function*() {
      const { layer, sockets } = harness();

      yield* Effect.gen(function*() {
        const chat = yield* Chat;
        const latest = yield* Ref.make<ReadonlyArray<string>>([]);
        const fiber = yield* Effect.forkScoped(
          Stream.runForEach(chat.typing(conversationId), (typing) => Ref.set(latest, typing)),
        );
        yield* settle;
        const live = sockets.at(-1)!;
        yield* settle;

        live.deliver(typingHint(driver));
        live.deliver(typingHint("someone-elsewhere", "c2"));
        yield* settle;
        expect(yield* Ref.get(latest)).toEqual([driver]);

        yield* TestClock.adjust("3 seconds");
        expect(yield* Ref.get(latest)).toEqual([driver]);
        yield* TestClock.adjust("3 seconds");
        expect(yield* Ref.get(latest)).toEqual([]);
        yield* Fiber.interrupt(fiber);
      }).pipe(Effect.scoped, Effect.provide(layer));
    }));
});
