import { AuthToken } from "@/AuthToken.js";
import { SurgeApi } from "@/SurgeApi.js";
import { describe, expect, it } from "@effect/vitest";
import { Coordinate, FareId } from "@surge/domain/trip/Trip";
import { Effect, Layer } from "effect";
import { FetchHttpClient } from "effect/unstable/http";

/**
 * The typed client against the real Go gateway.
 *
 * The contract test next door proves the declared routes match the generated
 * OpenAPI document. This proves the decoders match the bytes: proto3 JSON emits
 * int64 as a string, enums by name and unpopulated fields as zero values, and a
 * schema that disagrees with any of those fails here rather than three
 * components into a page.
 *
 * Skips when nothing is running, so `pnpm test` stays useful on a bare machine.
 */
const gateway = process.env["SURGE_API_URL"] ?? "http://localhost:8100";
const authBase = process.env["AUTH_BASE_URL"] ?? "http://localhost:3200";

/**
 * better-auth refuses a request whose Origin it does not trust, and Node's
 * fetch sends a null one where curl sends none at all — so a call that works
 * from a shell fails here with MISSING_OR_NULL_ORIGIN. Sending the web app's
 * origin is not a workaround: it is what the browser this test stands in for
 * would send.
 */
const webOrigin = process.env["WEB_URL"] ?? "http://localhost:5173";

const reachable = async (url: string): Promise<boolean> => {
  try {
    const response = await fetch(url, { signal: AbortSignal.timeout(2000) });
    return response.ok;
  } catch {
    return false;
  }
};

const online = await reachable(`${gateway}/health`) && await reachable(`${authBase}/health`);

/** A real rider, because the API has no other kind of caller. */
const signUp = async (): Promise<string> => {
  const email = `client-${Date.now()}@surge.test`;

  const signup = await fetch(`${authBase}/api/auth/sign-up/email`, {
    method: "POST",
    headers: { "content-type": "application/json", origin: webOrigin },
    body: JSON.stringify({
      email,
      password: "correct-horse-battery",
      name: "Client Test",
      role: "rider",
    }),
  });

  if (!signup.ok) {
    throw new Error(`sign-up failed with ${signup.status}: ${await signup.text()}`);
  }

  // Only the name=value part; a full Set-Cookie carries attributes the Cookie
  // header must not.
  const cookie = signup.headers.getSetCookie()
    .map((entry) => entry.split(";")[0])
    .join("; ");

  const token = await fetch(`${authBase}/api/auth/token`, {
    headers: { cookie, origin: webOrigin },
  });

  const body = (await token.json()) as { token?: string; };
  if (typeof body.token !== "string") {
    throw new Error(`no token in response: ${JSON.stringify(body)}`);
  }
  return body.token;
};

describe.skipIf(!online)("SurgeApi against the running gateway", () => {
  it.effect("previews a trip and decodes every fare", () =>
    Effect.gen(function*() {
      const api = yield* SurgeApi;

      const preview = yield* api.trips.preview({
        payload: {
          pickup: Coordinate.make({ lat: 52.3791, lng: 4.9003 }),
          dropoff: Coordinate.make({ lat: 52.36, lng: 4.8852 }),
        },
      });

      expect(preview.fares.length).toBeGreaterThan(0);
      expect(preview.route.meters).toBeGreaterThan(0);
      // A real Amsterdam route, not a straight line: if these were equal we
      // would be routing over something other than roads.
      expect(preview.route.polyline6.length).toBeGreaterThan(50);

      for (const fare of preview.fares) {
        // Decoded from the string proto3 emits for int64, so this is a number
        // here and was "1627" on the wire.
        expect(typeof fare.totalCents).toBe("number");
        expect(fare.totalCents).toBeGreaterThan(0);
      }
    }).pipe(Effect.provide(layer)));

  it.effect("books a trip, and a retry returns the same one", () =>
    Effect.gen(function*() {
      const api = yield* SurgeApi;

      const preview = yield* api.trips.preview({
        payload: {
          pickup: Coordinate.make({ lat: 52.3791, lng: 4.9003 }),
          dropoff: Coordinate.make({ lat: 52.36, lng: 4.8852 }),
        },
      });

      const key = `client-test-${Date.now()}`;
      const book = () =>
        api.trips.create({
          payload: { fareId: FareId.make(preview.fares[0]!.fareId) },
          headers: { "idempotency-key": key },
        });

      const first = yield* book();
      const again = yield* book();

      expect(first.trip.id).toBe(again.trip.id);
      expect(first.trip.status).toBe("TRIP_STATUS_REQUESTED");
      // Unassigned arrives as "" rather than absent, because the gateway emits
      // unpopulated fields — a schema expecting an optional would reject it.
      expect(first.trip.driverId).toBe("");
    }).pipe(Effect.provide(layer)));

  it.effect("lists only the caller's own trips", () =>
    Effect.gen(function*() {
      const api = yield* SurgeApi;
      const listed = yield* api.trips.list({ query: {} });

      // A fresh rider has booked nothing, and must not see anybody else's.
      // The list is scoped by the caller's token; there is no parameter for
      // whose history to read, which is the simplest way to ensure there is no
      // way to ask for someone else's.
      expect(listed.trips).toEqual([]);
      expect(listed.nextPageToken).toBe("");
    }).pipe(Effect.provide(freshRiderLayer())));
});

/** Wires the client the way `apps/web` does: platform layer at the edge. */
const layerFor = (token: string) =>
  SurgeApi.layer.pipe(
    Layer.provide(
      Layer.succeed(AuthToken)({
        get: Effect.succeed(token),
        identity: Effect.die("not needed here"),
        invalidate: Effect.void,
      }),
    ),
    Layer.provide(FetchHttpClient.layer),
  );

const layer = layerFor(online ? await signUp() : "");

/** A rider who has booked nothing, for the isolation check. */
const freshRiderLayer = () => layerFor(freshToken);
const freshToken = online ? await signUp() : "";
