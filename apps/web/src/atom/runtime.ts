import { browserGeolocation } from "@/lib/browser-geolocation.js";
import { AuthToken } from "@surge/client/AuthToken";
import { Realtime } from "@surge/client/Realtime";
import { SurgeApi } from "@surge/client/SurgeApi";
import { ConfigProvider, Layer } from "effect";
import { FetchHttpClient } from "effect/unstable/http";
import { Atom } from "effect/unstable/reactivity";
import { Socket } from "effect/unstable/socket";

/**
 * Where `packages/client`'s promise gets cashed.
 *
 * Everything under `packages/client` declares what it needs — `HttpClient`,
 * `Socket.WebSocketConstructor` — and never imports a platform package. This
 * file is the edge that provides the browser's versions of both, and it is the
 * only file in the repository that would need a React Native counterpart when
 * `apps/mobile` arrives. That is the entire point of the arrangement, and
 * `packages/client/test/platform-free.test.ts` is what keeps it true.
 *
 * One runtime, so one `AuthToken` cache and one WebSocket for the session. A
 * per-route runtime would mint a token per route and open a socket per route,
 * and the driver app would drop its connection every time the driver navigated.
 */

/**
 * The three values a browser is allowed to configure, and no more.
 *
 * Vite only exposes `VITE_`-prefixed variables to the client bundle, and it
 * replaces `import.meta.env.VITE_X` statically at build time — so the mapping
 * has to be written out rather than derived, and writing it out has the useful
 * side effect of being the list of what the browser knows. Everything else the
 * system is configured with stays on the servers that need it.
 *
 * Unset falls through to the defaults each service declares, which are the
 * local ones.
 */
const browserConfig = ConfigProvider.layer(
  ConfigProvider.fromEnvRecord({
    SURGE_API_URL: import.meta.env.VITE_SURGE_API_URL,
    SURGE_WS_URL: import.meta.env.VITE_SURGE_WS_URL,
    AUTH_BASE_URL: import.meta.env.VITE_AUTH_BASE_URL,
  }),
);

const platform = Layer.mergeAll(
  FetchHttpClient.layer,
  Socket.layerWebSocketConstructorGlobal,
);

/**
 * `provideMerge` rather than `provide`, so atoms can read the identity out of
 * the token they are already authenticating with instead of asking better-auth
 * a second question it has already answered.
 */
const services = Layer.mergeAll(SurgeApi.layer, Realtime.layer, browserGeolocation).pipe(
  Layer.provideMerge(AuthToken.layer),
  Layer.provide(platform),
  Layer.provide(browserConfig),
);

/**
 * Built lazily, on the first atom that reads it — which is why every route that
 * touches it renders `ssr: false`. Building this layer on the server would
 * open a WebSocket to the gateway from Node during server rendering, once per
 * request, for a socket nothing would ever read.
 */
export const runtime = Atom.runtime(services);
