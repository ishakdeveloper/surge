import { AuthToken, type AuthTokenService } from "@/AuthToken.js";
import { Realtime } from "@/Realtime.js";
import { describe, expect, it } from "@effect/vitest";
import { Identity } from "@surge/domain/iam/Identity";
import { Effect, Fiber, Layer, Stream } from "effect";
import { TestClock } from "effect/testing";
import { Socket } from "effect/unstable/socket";
import * as fs from "node:fs";
import * as path from "node:path";

/**
 * The realtime client, against a WebSocket that never leaves the process.
 *
 * The frames are the committed ones from `backend/shared/wire/testdata`, so
 * this exercises what Go actually emits rather than what this file imagines it
 * emits — the same fixtures `packages/domain/test/realtime/Wire.test.ts` and
 * `backend/shared/wire/wire_test.go` hold both languages to.
 *
 * What is worth testing here is not decoding, which those two already cover. It
 * is the connection: that a dropped socket reconnects and resumes delivering
 * offers, that a frame this build does not understand does not take the
 * connection down with it, and that the `(epoch, seq)` discipline lives in one
 * place instead of in every component that reports a position.
 */
const fixture = (name: string): string =>
  fs.readFileSync(
    path.join(
      import.meta.dirname,
      "..",
      "..",
      "..",
      "backend",
      "shared",
      "wire",
      "testdata",
      name,
    ),
    "utf8",
  );

const WELCOME = fixture("server_welcome.json");
const OFFER = fixture("server_offer.json");
const TRIP_UPDATED = fixture("server_trip_updated.json");

interface Listener {
  readonly fn: (event: unknown) => void;
  readonly once: boolean;
}

/**
 * Enough of a `WebSocket` for `Socket.fromWebSocket`, and no more.
 *
 * `readyState` starts open so nothing here depends on the ten-second open
 * timeout, which under a test clock would never fire anyway and under a real
 * one would only make the suite slow.
 */
class FakeWebSocket {
  readyState = 1;
  readonly sent: Array<string> = [];
  private readonly listeners = new Map<string, Array<Listener>>();

  constructor(readonly url: string) {}

  addEventListener(type: string, fn: (event: unknown) => void, options?: { once?: boolean; }) {
    const existing = this.listeners.get(type) ?? [];
    existing.push({ fn, once: options?.once === true });
    this.listeners.set(type, existing);
  }

  removeEventListener(type: string, fn: (event: unknown) => void) {
    this.listeners.set(
      type,
      (this.listeners.get(type) ?? []).filter((listener) => listener.fn !== fn),
    );
  }

  send(data: string) {
    this.sent.push(data);
  }

  close(code = 1000, reason = "") {
    this.drop(code, reason);
  }

  /** True once the socket's read loop is attached, which is the only reliable "ready". */
  get listening(): boolean {
    return (this.listeners.get("message") ?? []).length > 0;
  }

  deliver(frame: string) {
    this.emit("message", { data: frame });
  }

  /** What a gateway restart or a slow-consumer eviction looks like from here. */
  drop(code: number, reason: string) {
    if (this.readyState === 3) return;
    this.readyState = 3;
    this.emit("close", { code, reason });
  }

  private emit(type: string, event: unknown) {
    const listeners = this.listeners.get(type) ?? [];
    this.listeners.set(type, listeners.filter((listener) => !listener.once));
    for (const listener of listeners) listener.fn(event);
  }
}

const stubAuth: AuthTokenService = {
  get: Effect.succeed("a-token-nothing-here-verifies"),
  identity: Effect.succeed(
    new Identity({
      userId: Identity.fields.userId.make("drv-000123"),
      email: "driver@surge.test",
      emailVerified: true,
      role: "driver",
    }),
  ),
  invalidate: Effect.void,
};

/**
 * Lets every queued fiber run.
 *
 * `it.effect` schedules cooperatively on one thread, so yielding repeatedly is
 * how a test observes work that a forked fiber is partway through — connecting
 * a socket and subscribing to a `PubSub` each take several passes.
 */
const settle = Effect.forEach(
  Array.from({ length: 40 }, (_, index) => index),
  () => Effect.yieldNow,
  { discard: true },
);

const harness = () => {
  const sockets: Array<FakeWebSocket> = [];

  const layer = Realtime.layer.pipe(
    Layer.provide([
      Layer.succeed(Socket.WebSocketConstructor)((url) => {
        const socket = new FakeWebSocket(url);
        sockets.push(socket);
        return socket as unknown as globalThis.WebSocket;
      }),
      Layer.succeed(AuthToken)(stubAuth),
    ]),
  );

  /** The socket the client is currently on, once its read loop is attached. */
  const live = Effect.gen(function*() {
    yield* settle;
    const socket = sockets.at(-1);
    if (socket === undefined || !socket.listening) {
      return yield* Effect.die(new Error("the client never opened a socket"));
    }
    return socket;
  });

  return { sockets, layer, live };
};

describe("Realtime", () => {
  it.effect("carries the token on the handshake, because a browser cannot set a header", () =>
    Effect.gen(function*() {
      const { layer, live } = harness();

      yield* Effect.gen(function*() {
        yield* Realtime;
        const socket = yield* live;
        expect(socket.url).toBe(
          "ws://localhost:8100/ws?token=a-token-nothing-here-verifies",
        );
      }).pipe(Effect.provide(layer));
    }));

  it.effect("reports Connected on the gateway's welcome, not on the socket opening", () =>
    Effect.gen(function*() {
      const { layer, live } = harness();

      yield* Effect.gen(function*() {
        const realtime = yield* Realtime;
        const observed = yield* Effect.forkScoped(
          Stream.runCollect(Stream.take(realtime.status, 2)),
        );

        const socket = yield* live;
        // The socket has been open this whole time and the status is not
        // Connected yet — that is the distinction under test.
        socket.deliver(WELCOME);

        const states = yield* Fiber.join(observed);
        expect(Array.from(states)).toEqual(["Connecting", "Connected"]);
      }).pipe(Effect.provide(layer));
    }));

  it.effect("delivers an offer", () =>
    Effect.gen(function*() {
      const { layer, live } = harness();

      yield* Effect.gen(function*() {
        const realtime = yield* Realtime;
        const received = yield* Effect.forkScoped(
          Stream.runCollect(Stream.take(realtime.offers, 1)),
        );

        const socket = yield* live;
        yield* settle;
        socket.deliver(OFFER);

        const [offer] = Array.from(yield* Fiber.join(received));
        expect(offer?.tripId).toBe("0f2a6c1e-9d4b-4a77-8c31-6b1e5a2d9f80");
        expect(offer?.replyCell).toBe("871f1d492ffffff");
      }).pipe(Effect.provide(layer));
    }));

  it.effect("delivers trip updates apart from offers", () =>
    Effect.gen(function*() {
      const { layer, live } = harness();

      yield* Effect.gen(function*() {
        const realtime = yield* Realtime;
        const updates = yield* Effect.forkScoped(
          Stream.runCollect(Stream.take(realtime.tripUpdates, 1)),
        );
        const offers = yield* Effect.forkScoped(
          Stream.runCollect(Stream.take(realtime.offers, 1)),
        );

        const socket = yield* live;
        yield* settle;
        // Interleaved, as they are on the wire: ws.push carries both, and each
        // stream must take only its own.
        socket.deliver(TRIP_UPDATED);
        socket.deliver(OFFER);

        const [update] = Array.from(yield* Fiber.join(updates));
        expect(update?.status).toBe("TRIP_STATUS_ACCEPTED");
        expect(update?.tripId).toBe("0f2a6c1e-9d4b-4a77-8c31-6b1e5a2d9f80");

        const [offer] = Array.from(yield* Fiber.join(offers));
        expect(offer?.replyCell).toBe("871f1d492ffffff");
      }).pipe(Effect.provide(layer));
    }));

  it.effect("survives a frame it does not understand", () =>
    Effect.gen(function*() {
      const { layer, live } = harness();

      yield* Effect.gen(function*() {
        const realtime = yield* Realtime;
        const received = yield* Effect.forkScoped(
          Stream.runCollect(Stream.take(realtime.offers, 1)),
        );

        const socket = yield* live;
        yield* settle;
        // A future gateway publishing a message this build predates. Dropping
        // it is the point: tearing down a connection carrying live offers
        // because of one unrecognised frame is a worse outcome than ignoring it.
        socket.deliver(`{"_tag":"SurgeUpdated","cell":"871f1d492ffffff","multiplier":1.4}`);
        socket.deliver("not json at all");
        socket.deliver(OFFER);

        const [offer] = Array.from(yield* Fiber.join(received));
        expect(offer?.driverId).toBe("drv-000123");
      }).pipe(Effect.provide(layer));
    }));

  it.effect("reconnects when the gateway drops the socket, and resumes", () =>
    Effect.gen(function*() {
      const { sockets, layer, live } = harness();

      yield* Effect.gen(function*() {
        const realtime = yield* Realtime;
        const received = yield* Effect.forkScoped(
          Stream.runCollect(Stream.take(realtime.offers, 1)),
        );

        const first = yield* live;
        yield* settle;

        // 1008 is what the gateway sends when it evicts a slow consumer, and it
        // is the case most worth reconnecting from: the client fell behind, and
        // a fresh connection is the recovery rather than the problem.
        first.drop(1008, "slow_consumer");

        // The backoff starts at half a second, jittered. On the test clock
        // waiting it out costs nothing.
        yield* TestClock.adjust("2 seconds");

        const second = yield* live;
        expect(second).not.toBe(first);
        expect(sockets.length).toBeGreaterThan(1);

        yield* settle;
        second.deliver(OFFER);

        const [offer] = Array.from(yield* Fiber.join(received));
        expect(offer?.tripId).toBe("0f2a6c1e-9d4b-4a77-8c31-6b1e5a2d9f80");
      }).pipe(Effect.provide(layer));
    }));

  it.effect("owns the epoch and sequence so no call site has to", () =>
    Effect.gen(function*() {
      const { layer, live } = harness();

      yield* Effect.gen(function*() {
        const realtime = yield* Realtime;
        const socket = yield* live;

        const position = { lat: 52.3702, lng: 4.8952, heading: 90, speedMps: 8 } as const;
        yield* realtime.ping({ ...position, status: "idle" });
        yield* realtime.ping({ ...position, status: "idle" });
        yield* settle;

        const pings = socket.sent.map((frame) => JSON.parse(frame));
        expect(pings.map((frame) => frame._tag)).toEqual(["ClientPing", "ClientPing"]);
        expect(pings.map((frame) => frame.ping.seq)).toEqual([1, 2]);
        expect(pings[0].ping.epoch).toBe(pings[1].ping.epoch);

        // The gateway fills this in from the verified token. A client that
        // could name its own driver id could report positions for somebody
        // else, and be dispatched their rides.
        expect(pings[0].ping).not.toHaveProperty("driverId");
      }).pipe(Effect.provide(layer));
    }));

  it.effect("answers an offer with the cell the offer arrived on", () =>
    Effect.gen(function*() {
      const { layer, live } = harness();

      yield* Effect.gen(function*() {
        const realtime = yield* Realtime;
        const received = yield* Effect.forkScoped(
          Stream.runCollect(Stream.take(realtime.offers, 1)),
        );

        const socket = yield* live;
        yield* settle;
        socket.deliver(OFFER);

        const [offer] = Array.from(yield* Fiber.join(received));
        yield* realtime.reply(offer!, true);
        yield* settle;

        const reply = JSON.parse(socket.sent.at(-1)!);
        expect(reply).toEqual({
          _tag: "ClientOfferReply",
          reply: { tripId: "0f2a6c1e-9d4b-4a77-8c31-6b1e5a2d9f80", accepted: true },
          // Not the driver's current cell — the shard that made the
          // reservation is the only one that can resolve the offer.
          replyCell: "871f1d492ffffff",
        });
      }).pipe(Effect.provide(layer));
    }));
});
