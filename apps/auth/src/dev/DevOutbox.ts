import { Clock, Context, Effect, Layer, Option, Ref } from "effect";

/**
 * The last code sent to each address or number, when nothing really sent it.
 *
 * Without a Resend key or an SMS provider, codes are written to the log — which
 * is enough for a person, and useless to an end-to-end test that has to type a
 * code into a phone. So the logging senders also leave the message here, and
 * `DevOutboxHttp` serves it back when `AUTH_DEV_OUTBOX=true`.
 *
 * Only the logging senders write to it. A configured provider delivers the code
 * for real and records nothing, so turning the endpoint on in a deployment
 * that sends mail exposes nothing — and it is off unless asked for.
 */
export interface OutboxEntry {
  readonly to: string;
  readonly text: string;
  readonly atMs: number;
}

export interface DevOutboxService {
  readonly record: (to: string, text: string) => Effect.Effect<void>;
  readonly latest: (to: string) => Effect.Effect<Option.Option<OutboxEntry>>;
}

/** Bounded, so a long-running dev server does not keep every code it ever sent. */
const CAPACITY = 200;

/** Addresses compare case-insensitively, as better-auth stores them. */
const key = (to: string) => to.trim().toLowerCase();

export class DevOutbox extends Context.Service<DevOutbox, DevOutboxService>()("DevOutbox") {
  static layer: Layer.Layer<DevOutbox> = Layer.effect(DevOutbox)(
    Effect.gen(function*() {
      const entries = yield* Ref.make<ReadonlyMap<string, OutboxEntry>>(new Map());

      return {
        record: (to, text) =>
          Effect.flatMap(Clock.currentTimeMillis, (atMs) =>
            Ref.update(entries, (current) => {
              const next = [...current].filter(([existing]) => existing !== key(to));
              return new Map([...next, [key(to), { to, text, atMs }] as const].slice(-CAPACITY));
            })),
        latest: (to) =>
          Effect.map(Ref.get(entries), (current) => Option.fromNullishOr(current.get(key(to)))),
      };
    }),
  );
}

/**
 * What a logging sender calls at build time: records into the outbox when the
 * service provides one, and does nothing otherwise — so a sender never has to
 * require it, and the tests that build a sender alone need not provide it.
 */
export const outboxRecorder = Effect.map(
  Effect.serviceOption(DevOutbox),
  (outbox) => (to: string, text: string): Effect.Effect<void> =>
    Option.match(outbox, { onNone: () => Effect.void, onSome: (found) => found.record(to, text) }),
);
