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
 * The three values a browser is allowed to configure, and no more.
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
  }),
);

/**
 * Credentials on every request, because auth and the gateway are never this
 * page's own origin — another port here, another subdomain deployed.
 *
 * `fetch` sends cookies only to its own origin unless told otherwise, so
 * without this the token exchange reached auth with no session cookie, got a
 * 401, and every product call failed for want of a token. The gateway answers
 * credentialed requests for the web origin (`cors.go`), and auth trusts it.
 */
const credentialed = Layer.succeed(FetchHttpClient.RequestInit)({ credentials: "include" });

const browser = Layer.mergeAll(
  FetchHttpClient.layer.pipe(Layer.provide(credentialed)),
  Socket.layerWebSocketConstructorGlobal,
  browserGeolocation,
);

export const webPlatform: Platform = {
  layer: AuthToken.layer.pipe(Layer.provideMerge(browser), Layer.provideMerge(browserConfig)),
};
