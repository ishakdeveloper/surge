import { Effect } from "effect";

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
export class FakeWebSocket {
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

/**
 * Lets every queued fiber run.
 *
 * `it.effect` schedules cooperatively on one thread, so yielding repeatedly is
 * how a test observes work that a forked fiber is partway through — connecting
 * a socket and subscribing to a `PubSub` each take several passes.
 */
export const settle = Effect.forEach(
  Array.from({ length: 40 }, (_, index) => index),
  () => Effect.yieldNow,
  { discard: true },
);
