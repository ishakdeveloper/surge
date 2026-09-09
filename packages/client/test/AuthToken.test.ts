import { AuthToken, TokenUnavailable } from "@/AuthToken.js";
import { describe, expect, it } from "@effect/vitest";
import { Effect, Encoding, Layer, Ref } from "effect";
import { HttpClient, HttpClientResponse } from "effect/unstable/http";

/** Builds a JWT-shaped string. Unsigned: nothing here verifies it, Go does. */
const tokenWith = (claims: Record<string, unknown>): string => {
  const encode = (value: unknown) => Encoding.encodeBase64Url(JSON.stringify(value));
  return `${encode({ alg: "EdDSA" })}.${encode(claims)}.signature-not-checked-here`;
};

/**
 * Expiries are relative to the TEST clock, which starts at zero.
 *
 * `it.effect` installs a TestClock, so `Clock.currentTimeMillis` is 0 rather
 * than the wall clock. A fixture built from `Date.now()` therefore looks
 * ~1.8 billion seconds in the future no matter what it says, and the expiry
 * test silently passes for the wrong reason.
 */
/** Comfortably beyond the one-minute refresh margin, on the test clock. */
const FRESH = 900;
/** Already past, so the refresh margin is not what is under test. */
const EXPIRED = -10;

const validClaims = (expSeconds: number) => ({
  userId: "user-123",
  email: "driver@surge.test",
  emailVerified: true,
  role: "driver",
  exp: expSeconds,
  iss: "http://localhost:3200",
  aud: "surge",
});

/**
 * A stub auth service that counts how many times it was asked.
 *
 * The call count is the point of most of these tests: the whole reason this
 * service exists rather than fetching a token per request is that it should
 * not fetch a token per request.
 */
const stubAuth = (responses: ReadonlyArray<{ status: number; body: unknown; }>) =>
  Effect.gen(function*() {
    const calls = yield* Ref.make(0);

    const client = HttpClient.make((request) =>
      Effect.gen(function*() {
        const index = yield* Ref.getAndUpdate(calls, (n) => n + 1);
        const response = responses[Math.min(index, responses.length - 1)]!;

        return HttpClientResponse.fromWeb(
          request,
          new Response(JSON.stringify(response.body), {
            status: response.status,
            headers: { "content-type": "application/json" },
          }),
        );
      })
    );

    return { client, calls } as const;
  });

/** The service under test, wired to a stub transport. */
const withStub = (responses: ReadonlyArray<{ status: number; body: unknown; }>) =>
  Effect.gen(function*() {
    const stub = yield* stubAuth(responses);

    const auth = yield* Effect.provide(
      AuthToken,
      AuthToken.layer.pipe(
        Layer.provide(Layer.succeed(HttpClient.HttpClient)(stub.client)),
      ),
    );

    return { auth, calls: stub.calls } as const;
  });

describe("AuthToken", () => {
  it.effect("mints a token and decodes the identity Go will read", () =>
    Effect.gen(function*() {
      const token = tokenWith(validClaims(FRESH));

      const { auth } = yield* withStub([{ status: 200, body: { token } }]);
      const identity = yield* auth.identity;

      expect(identity.userId).toBe("user-123");
      expect(identity.email).toBe("driver@surge.test");
      // The role has to survive the round trip: it is what every Go
      // authorisation check reads out of the token.
      expect(identity.role).toBe("driver");
    }));

  it.effect("caches, so a page of requests is one token fetch", () =>
    Effect.gen(function*() {
      const token = tokenWith(validClaims(FRESH));

      const { auth, calls } = yield* withStub([{ status: 200, body: { token } }]);

      yield* auth.get;
      yield* auth.get;
      yield* auth.get;

      expect(yield* Ref.get(calls)).toBe(1);
    }));

  it.effect("refetches once the token is close to expiry", () =>
    Effect.gen(function*() {
      // Already expired, so the refresh margin is irrelevant and the decision
      // is unambiguous.
      const stale = tokenWith(validClaims(EXPIRED));
      const fresh = tokenWith(validClaims(FRESH));

      const { auth, calls } = yield* withStub([
        { status: 200, body: { token: stale } },
        { status: 200, body: { token: fresh } },
      ]);

      yield* auth.get;
      yield* auth.get;

      expect(yield* Ref.get(calls)).toBe(2);
    }));

  it.effect("reports a signed-out visitor as NoSession rather than failing loudly", () =>
    Effect.gen(function*() {
      const { auth } = yield* withStub([{ status: 401, body: { message: "unauthorized" } }]);
      const result = yield* Effect.flip(auth.get);

      expect(result).toBeInstanceOf(TokenUnavailable);
      expect(result.reason).toBe("NoSession");
    }));

  it.effect("rejects a token whose claims do not match the contract", () =>
    Effect.gen(function*() {
      // `role: "superuser"` is not in the union apps/auth mints or pkg/authz
      // accepts. A client that shrugged at this would send Go a token it is
      // about to reject, and the failure would surface three services away.
      const token = tokenWith({
        ...validClaims(FRESH),
        role: "superuser",
      });

      const { auth } = yield* withStub([{ status: 200, body: { token } }]);
      const result = yield* Effect.flip(auth.get);

      expect(result.reason).toBe("Malformed");
    }));

  it.effect("invalidate forces the next call to refetch", () =>
    Effect.gen(function*() {
      const token = tokenWith(validClaims(FRESH));

      const { auth, calls } = yield* withStub([{ status: 200, body: { token } }]);

      yield* auth.get;
      yield* auth.invalidate;
      yield* auth.get;

      expect(yield* Ref.get(calls)).toBe(2);
    }));
});
