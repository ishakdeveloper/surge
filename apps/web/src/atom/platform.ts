import { browserGeolocation } from "@/lib/browser-geolocation.js";
import { publicConfig } from "@/lib/public-config.js";
import { AuthToken } from "@surge/client/AuthToken";
import type { Platform } from "@surge/common/atom/runtime";
import { ConfigProvider, Layer } from "effect";
import { FetchHttpClient } from "effect/unstable/http";
import { Socket } from "effect/unstable/socket";

/**
 * The browser's half of the shared runtime in `@surge/common/atom/runtime`.
 *
 * `packages/client` declares what it needs — `HttpClient`,
 * `Socket.WebSocketConstructor`, `Geolocation` — and never imports a platform
 * package. This is the browser's answer, seeded into the registry in
 * `routes/__root.tsx`; `apps/mobile/src/atom/platform.ts` is the phone's.
 *
 * Nothing here is built on import. The runtime builds it lazily, on the first
 * atom that reads it — which is why every route that touches it renders
 * `ssr: false`: building it on the server would open a WebSocket to the
 * gateway from Node during server rendering, for a socket nothing would read.
 */

/**
 * The four values a browser is allowed to configure, and no more.
 *
 * Read from `publicConfig`, which the server resolved from its environment when
 * it rendered the page — so one image serves every environment. Writing the
 * mapping out is the list of what the browser knows. Unset falls through to the
 * defaults each service declares, which are the local ones.
 */
const browserConfig = ConfigProvider.layer(
  ConfigProvider.fromEnvRecord({
    SURGE_API_URL: publicConfig.SURGE_API_URL,
    SURGE_WS_URL: publicConfig.SURGE_WS_URL,
    AUTH_BASE_URL: publicConfig.AUTH_BASE_URL,
    // Address search. Unset, it is the public Photon instance, which is
    // fair-use only and answers some clients with a 503; a deployment points
    // this at its own.
    PLACES_URL: publicConfig.PLACES_URL,
  }),
);

/**
 * The client `AuthToken` trades the session for a JWT with.
 *
 * Auth runs on its own origin, and a cross-origin fetch leaves cookies behind
 * unless it is told otherwise — so without this the token request arrives
 * without the session and every signed-in page is refused. This one client
 * sends them, and only this one: the gateway wants a bearer token, not a
 * cookie, and Photon wants nothing. Auth answers the web origin with
 * credentials allowed (`apps/auth/src/Main.ts`), which is what lets the
 * browser hand the response back. `apps/mobile/src/atom/platform.ts` does the
 * same job by attaching the cookie itself.
 *
 * `Layer.fresh`, because layers are memoised by reference and
 * `FetchHttpClient.layer` keeps the context it was built in. Shared, the one
 * instance would carry `credentials: "include"` to every request — and Photon,
 * which answers `Access-Control-Allow-Origin: *`, refuses every credentialed
 * one, so address search would fail for everyone signed in.
 */
const sessionClient = Layer.fresh(FetchHttpClient.layer).pipe(
  Layer.provide(Layer.succeed(FetchHttpClient.RequestInit)({ credentials: "include" })),
);

const browser = Layer.mergeAll(
  FetchHttpClient.layer,
  Socket.layerWebSocketConstructorGlobal,
  browserGeolocation,
);

export const webPlatform: Platform = {
  layer: Layer.mergeAll(AuthToken.layer.pipe(Layer.provide(sessionClient)), browser).pipe(
    Layer.provideMerge(browserConfig),
  ),
};
