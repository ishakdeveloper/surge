import { Effect, Option } from "effect";
import { HttpRouter, HttpServerRequest, HttpServerResponse } from "effect/unstable/http";
import { RateLimiter } from "effect/unstable/persistence";
import { Auth } from "./Auth.js";

/**
 * Endpoints where throttling *is* the security boundary.
 *
 * Every way in is a six-digit code, about twenty bits, so it is only as strong
 * as the number of attempts allowed — better-auth burns a code after three
 * wrong answers, and this bounds how many codes a caller can ask for. The same
 * limit is what stops a script turning the send endpoints into free email and,
 * worse, paid SMS to numbers it chooses.
 *
 * `/sign-in` covers `/sign-in/email-otp`; `/phone-number` covers both
 * `send-otp` and `verify`.
 */
const CREDENTIAL_PATHS = [
  "/api/auth/sign-in",
  "/api/auth/email-otp",
  "/api/auth/phone-number",
];

const isCredentialPath = (url: string) => CREDENTIAL_PATHS.some((path) => url.startsWith(path));

/** Per-caller identity for the limiter. Falls back to a shared bucket. */
const callerKey = (request: HttpServerRequest.HttpServerRequest) => {
  const forwarded = request.headers["x-forwarded-for"];
  const address = forwarded ?? Option.getOrUndefined(request.remoteAddress) ?? "unknown";

  return `auth:${address.split(",")[0]?.trim() ?? "unknown"}:${request.url.split("?")[0]}`;
};

/**
 * Mounts better-auth's own routes under `/api/auth/*`.
 *
 * better-auth speaks web `Request`/`Response`, and v4 converts both ways, so
 * this is a bridge rather than a reimplementation of its endpoints.
 */
export const AuthHttp = HttpRouter.add(
  "*",
  "/api/auth/*",
  Effect.fnUntraced(function*(request: HttpServerRequest.HttpServerRequest) {
    const auth = yield* Auth;

    if (request.method === "POST" && isCredentialPath(request.url)) {
      const limiter = yield* RateLimiter.RateLimiter;

      const allowed = yield* limiter.consume({
        key: callerKey(request),
        limit: 10,
        window: "1 minute",
        onExceeded: "fail",
      }).pipe(
        Effect.as(true),
        // A store failure must not become an outage on the sign-in path, and a
        // exceeded limit is the caller's problem, not a defect.
        Effect.catchTag(
          "RateLimiterError",
          (error) => Effect.succeed(error.reason._tag !== "RateLimitExceeded"),
        ),
      );

      if (!allowed) {
        return HttpServerResponse.text("Too many requests", { status: 429 });
      }
    }

    const webRequest = yield* HttpServerRequest.toWeb(request);
    const webResponse = yield* Effect.promise(() => auth.handler(webRequest));

    return HttpServerResponse.fromWeb(webResponse);
  }),
);
