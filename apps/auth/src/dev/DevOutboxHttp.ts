import { Config, Effect, Layer, Option } from "effect";
import { HttpRouter, HttpServerRequest, HttpServerResponse } from "effect/unstable/http";
import { DevOutbox } from "./DevOutbox.js";

/** The code inside a message: the first run of exactly six digits. */
const CODE = /\b(\d{6})\b/;

/**
 * `GET /dev/outbox?to=…` — the last code the log-only senders "sent" there.
 *
 * What `apps/mobile/e2e` reads to type a sign-in code, since a Maestro flow
 * cannot read the auth service's log. Off unless `AUTH_DEV_OUTBOX=true`; with
 * the flag off the route does not exist at all, rather than answering "no".
 */
const route = HttpRouter.add(
  "GET",
  "/dev/outbox",
  Effect.fnUntraced(function*(request: HttpServerRequest.HttpServerRequest) {
    const outbox = yield* DevOutbox;
    const to = new URL(request.url, "http://outbox.invalid").searchParams.get("to") ?? "";
    // Distinct from the 404 a closed outbox gives, which is how a test tells
    // whether it can read codes at all.
    if (to === "") {
      return HttpServerResponse.text("Ask for an address or a number: ?to=…", { status: 400 });
    }
    const entry = yield* outbox.latest(to);

    return Option.match(entry, {
      onNone: () => HttpServerResponse.text("Nothing has been sent there.", { status: 404 }),
      onSome: (found) =>
        HttpServerResponse.jsonUnsafe({
          to: found.to,
          code: CODE.exec(found.text)?.[1] ?? null,
          text: found.text,
          atMs: found.atMs,
        }),
    });
  }),
);

const off: typeof route = Layer.empty;

export const DevOutboxHttp = Layer.unwrap(
  Effect.gen(function*() {
    const enabled = yield* Config.boolean("AUTH_DEV_OUTBOX").pipe(Config.withDefault(false));
    return enabled ? route : off;
  }),
);
