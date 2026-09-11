import { AuthToken } from "@surge/client/AuthToken";
import { Geolocation } from "@surge/client/Geolocation";
import type { LatLng } from "@surge/domain/geo/Polyline";
import { Effect, Layer, Stream } from "effect";
import { type HttpBody, HttpClient, HttpClientResponse } from "effect/unstable/http";
import { Socket } from "effect/unstable/socket";
import type { Platform } from "../atom/runtime.js";

/**
 * A platform for tests, seeded into a registry the way each app seeds its own.
 *
 * Only the true boundary is fake — HTTP, the socket, the token source and the
 * device's position — so the shared atoms, the generated API client and the
 * screens above them run as they do in production. Responses come from
 * `routes`, matched on method and path; anything else answers the gateway's own
 * `not_found` body, so a test that forgot a route fails the way the app would.
 */
export interface FakeRoute {
  readonly method: string;
  readonly path: string;
  readonly status?: number;
  /** The JSON the gateway would send — responses captured from it are the best source. */
  readonly body: unknown;
}

export interface FakeRequest {
  readonly method: string;
  readonly path: string;
  readonly authorization: string | undefined;
  /** The JSON body, when there was one. */
  readonly body: unknown;
}

/** A socket that never opens: realtime builds, and nothing arrives. */
class SilentSocket extends EventTarget {
  readonly readyState = 0;
  close() {}
  send() {}
}

const jsonBody = (body: HttpBody.HttpBody): unknown =>
  body._tag === "Uint8Array" ? JSON.parse(new TextDecoder().decode(body.body)) : undefined;

export const fakePlatform = (options: {
  readonly routes: ReadonlyArray<FakeRoute>;
  /** Called with every request, for tests that assert on what was sent. */
  readonly onRequest?: (request: FakeRequest) => void;
  /** Where the device is, for `Geolocation.current`. Absent means no fix. */
  readonly position?: LatLng;
}): Platform => ({
  layer: Layer.mergeAll(
    Layer.succeed(AuthToken)({
      get: Effect.succeed("fake-token"),
      identity: Effect.die("the fake platform has no identity; seed the session atom instead"),
      invalidate: Effect.void,
    }),
    Layer.succeed(HttpClient.HttpClient)(
      HttpClient.make((request, url) => {
        options.onRequest?.({
          method: request.method,
          path: url.pathname,
          authorization: request.headers["authorization"],
          body: jsonBody(request.body),
        });
        const route = options.routes.find((candidate) =>
          candidate.method === request.method && candidate.path === url.pathname
        );
        const body = route === undefined
          ? {
            error: {
              code: "not_found",
              message: `no fake route for ${request.method} ${url.pathname}`,
            },
          }
          : route.body;

        return Effect.succeed(
          HttpClientResponse.fromWeb(
            request,
            new Response(JSON.stringify(body), {
              status: route === undefined ? 404 : route.status ?? 200,
              headers: { "content-type": "application/json" },
            }),
          ),
        );
      }),
    ),
    Layer.succeed(Socket.WebSocketConstructor)(
      // The constructor's contract is a WebSocket; this one only has to exist.
      // oxlint-disable-next-line typescript/no-unsafe-type-assertion
      () => new SilentSocket() as unknown as globalThis.WebSocket,
    ),
    Layer.succeed(Geolocation)({
      current: options.position === undefined
        ? Effect.die("the fake platform was given no position")
        : Effect.succeed(options.position),
      watch: Stream.empty,
    }),
  ),
});
