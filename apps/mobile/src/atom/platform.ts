import { authClient } from "@/iam/auth-client.js";
import { serviceUrls } from "@/lib/config.js";
import { nativeGeolocation } from "@/lib/native-geolocation.js";
import { AuthToken } from "@surge/client/AuthToken";
import type { Platform } from "@surge/common/atom/runtime";
import { ConfigProvider, Effect, Layer } from "effect";
import { FetchHttpClient, HttpClient, HttpClientRequest } from "effect/unstable/http";
import { Socket } from "effect/unstable/socket";

/**
 * The phone's half of the shared runtime in `@surge/common/atom/runtime` —
 * `apps/web/src/atom/platform.ts` is the browser's, and this file is the whole
 * difference between them. Seeded into each registry in
 * `iam/session-scope.tsx`.
 *
 * React Native ships WHATWG `fetch` and `WebSocket` globals, so the platform
 * layers are Effect's own fetch client and global WebSocket constructor, not a
 * React Native package.
 */

/** `packages/client` reads its addresses through `Config`; these are the phone's. */
const config = ConfigProvider.layer(ConfigProvider.fromEnvRecord(serviceUrls));

/**
 * React Native's fetch, without its native cookie jar.
 *
 * NSURLSession and OkHttp both keep cookies behind JavaScript's back and send
 * them unasked. The session cookie's one owner is better-auth's Expo client,
 * in SecureStore; a second copy in the platform jar could outlive a sign-out
 * and speak for whoever was signed in last.
 */
const fetchClient = FetchHttpClient.layer.pipe(
  Layer.provide(Layer.succeed(FetchHttpClient.RequestInit)({ credentials: "omit" })),
);

/**
 * The client `AuthToken` trades the session for a JWT with.
 *
 * The browser sends its cookie ambiently; a phone has to be told to. So this
 * one client — and only this one, since the Go services want a bearer token
 * and not a cookie — carries the session cookie that better-auth's Expo client
 * keeps, read fresh on each request so a sign-in is seen at once.
 */
const sessionClient = Layer.effect(HttpClient.HttpClient)(
  Effect.map(HttpClient.HttpClient, (client) =>
    HttpClient.mapRequest(client, (request) => {
      const cookie = authClient.getCookie();
      return cookie === "" ? request : request.pipe(HttpClientRequest.setHeader("cookie", cookie));
    })),
).pipe(Layer.provide(fetchClient));

export const mobilePlatform: Platform = {
  layer: Layer.mergeAll(
    AuthToken.layer.pipe(Layer.provide(sessionClient)),
    fetchClient,
    Socket.layerWebSocketConstructorGlobal,
    nativeGeolocation,
  ).pipe(Layer.provideMerge(config)),
};
