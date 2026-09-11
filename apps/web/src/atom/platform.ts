import { browserGeolocation } from "@/lib/browser-geolocation.js";
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
 * Vite only exposes `VITE_`-prefixed variables to the client bundle, and it
 * replaces `import.meta.env.VITE_X` statically at build time — so the mapping
 * has to be written out rather than derived, and writing it out has the useful
 * side effect of being the list of what the browser knows. Unset falls through
 * to the defaults each service declares, which are the local ones.
 */
const browserConfig = ConfigProvider.layer(
  ConfigProvider.fromEnvRecord({
    SURGE_API_URL: import.meta.env.VITE_SURGE_API_URL,
    SURGE_WS_URL: import.meta.env.VITE_SURGE_WS_URL,
    AUTH_BASE_URL: import.meta.env.VITE_AUTH_BASE_URL,
  }),
);

const browser = Layer.mergeAll(
  FetchHttpClient.layer,
  Socket.layerWebSocketConstructorGlobal,
  browserGeolocation,
);

export const webPlatform: Platform = {
  layer: AuthToken.layer.pipe(Layer.provideMerge(browser), Layer.provideMerge(browserConfig)),
};
