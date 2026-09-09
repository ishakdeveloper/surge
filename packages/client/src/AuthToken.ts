import { Identity } from "@surge/domain/iam/Identity";
import {
  Clock,
  Config,
  Context,
  Duration,
  Effect,
  Encoding,
  Layer,
  Ref,
  Result,
  Schema,
} from "effect";
import { HttpClient, HttpClientRequest } from "effect/unstable/http";

/**
 * The bridge between better-auth and Go.
 *
 * Every product API in this system is Go, and Go learns who the caller is from
 * a short-lived EdDSA token rather than from a session cookie — it verifies the
 * signature locally against `/api/auth/jwks`, so no request path touches Node
 * or its database. This service is what obtains that token and keeps it fresh.
 *
 * Deliberately platform-free: it declares `HttpClient` as a requirement and
 * never imports `@effect/platform-browser` or a `fetch` global. That is what
 * lets `apps/mobile` reuse it unchanged by providing React Native's HTTP layer
 * instead of the browser's.
 */
export class TokenUnavailable extends Schema.TaggedError<TokenUnavailable>()("TokenUnavailable", {
  reason: Schema.Literals(["NoSession", "Unreachable", "Malformed"]),
}) {}

/** The claims Go reads. `Identity` plus the expiry the cache needs. */
const Claims = Schema.Struct({
  userId: Schema.String,
  email: Schema.String,
  emailVerified: Schema.Boolean,
  role: Schema.Literals(["rider", "driver", "ops"]),
  exp: Schema.Number,
}).annotate({ identifier: "Claims" });

interface Cached {
  readonly token: string;
  readonly identity: Identity;
  /** Unix seconds. */
  readonly expiresAt: number;
}

/**
 * Refreshed this long before expiry.
 *
 * The token lives 15 minutes, so a minute of headroom costs nothing and covers
 * the case that actually bites: a request that passes the freshness check and
 * then spends a few seconds in flight, arriving at a Go service after the
 * signature has expired.
 */
const REFRESH_MARGIN = Duration.minutes(1);

export interface AuthTokenService {
  /** A currently valid token, minting a new one if needed. */
  readonly get: Effect.Effect<string, TokenUnavailable>;
  /** The identity carried by the current token. */
  readonly identity: Effect.Effect<Identity, TokenUnavailable>;
  /** Drops the cache. Call on sign-out, or after a 401. */
  readonly invalidate: Effect.Effect<void>;
}

export class AuthToken extends Context.Service<AuthToken, AuthTokenService>()("AuthToken") {
  static layer: Layer.Layer<AuthToken, never, HttpClient.HttpClient> = Layer.effect(AuthToken)(
    Effect.gen(function*() {
      const baseUrl = yield* Config.nonEmptyString("AUTH_BASE_URL").pipe(
        Config.withDefault("http://localhost:3200"),
      );
      const client = yield* HttpClient.HttpClient;
      const cache = yield* Ref.make<Cached | undefined>(undefined);

      const fetchToken = Effect.gen(function*() {
        const response = yield* client.execute(
          HttpClientRequest.get(`${baseUrl}/api/auth/token`).pipe(
            HttpClientRequest.setHeader("accept", "application/json"),
          ),
        ).pipe(
          Effect.catchTag("HttpClientError", () => new TokenUnavailable({ reason: "Unreachable" })),
        );

        // Checked rather than filtered: better-auth answers 401 for "no
        // session", which is a normal state for a signed-out visitor and not a
        // transport failure.
        if (response.status !== 200) {
          return yield* new TokenUnavailable({ reason: "NoSession" });
        }

        const body = yield* response.json.pipe(
          Effect.catchTag("HttpClientError", () => new TokenUnavailable({ reason: "Malformed" })),
        );

        const token = typeof body === "object" && body !== null && "token" in body
          ? (body as { token: unknown; }).token
          : undefined;

        if (typeof token !== "string" || token === "") {
          return yield* new TokenUnavailable({ reason: "NoSession" });
        }

        const claims = yield* decodeClaims(token);

        const cached: Cached = {
          token,
          identity: new Identity({
            userId: Identity.fields.userId.make(claims.userId),
            email: claims.email,
            emailVerified: claims.emailVerified,
            role: claims.role,
          }),
          expiresAt: claims.exp,
        };

        yield* Ref.set(cache, cached);
        return cached;
      });

      const current = Effect.gen(function*() {
        const existing = yield* Ref.get(cache);
        const now = yield* Clock.currentTimeMillis;

        const stillFresh = existing !== undefined
          && existing.expiresAt * 1000 - Duration.toMillis(REFRESH_MARGIN) > now;

        return stillFresh ? existing : yield* fetchToken;
      });

      return {
        get: Effect.map(current, (cached) => cached.token),
        identity: Effect.map(current, (cached) => cached.identity),
        invalidate: Ref.set(cache, undefined),
      };
    }),
    // A ConfigError here means AUTH_BASE_URL is set to something unusable, and
    // there is no client behaviour that recovers from not knowing where the
    // auth service is.
  ).pipe(Layer.orDie);
}

/**
 * Reads the claims out of a JWT without verifying it.
 *
 * Not a security decision — the browser is not the party that verifies this
 * token. Go does, against the JWKS, and it is the only verification that
 * matters. This exists so the client knows when its own token expires and who
 * it belongs to, and treating a malformed one as "no session" is the right
 * outcome regardless.
 *
 * `Encoding` rather than `atob` or `Buffer`, because this package must not
 * assume a browser or Node.
 */
const decodeClaims = Effect.fnUntraced(function*(token: string) {
  const payload = token.split(".")[1];
  if (payload === undefined) {
    return yield* new TokenUnavailable({ reason: "Malformed" });
  }

  const decoded = Encoding.decodeBase64UrlString(payload);
  if (Result.isFailure(decoded)) {
    return yield* new TokenUnavailable({ reason: "Malformed" });
  }

  const parsed = yield* Effect.try({
    try: () => JSON.parse(decoded.success) as unknown,
    catch: () => new TokenUnavailable({ reason: "Malformed" }),
  });

  return yield* Schema.decodeUnknownEffect(Claims)(parsed).pipe(
    Effect.catchTag("SchemaError", () => new TokenUnavailable({ reason: "Malformed" })),
  );
});

/**
 * An HttpClient that carries the token on every request.
 *
 * Applied once here so no call site ever hand-sets an Authorization header, and
 * so a token that expires mid-session is refreshed underneath the caller rather
 * than surfacing as a 401 they have to think about.
 */
export const withAuth = Effect.fnUntraced(function*(client: HttpClient.HttpClient) {
  const auth = yield* AuthToken;

  return HttpClient.mapRequestEffect(client, (request) =>
    auth.get.pipe(
      Effect.map((token) => HttpClientRequest.bearerToken(request, token)),
      // An unauthenticated caller still gets to make the request; the Go
      // service answers 401 and the UI decides what that means. Failing here
      // would turn every anonymous read into a client-side error.
      Effect.orElseSucceed(() => request),
    ));
});
