/**
 * The browser's whole configuration surface, typed.
 *
 * Vite exposes only `VITE_`-prefixed variables to the client bundle, and types
 * every one of them `any` unless told otherwise. Declaring the three this app
 * reads turns a misspelled name into a compile error rather than an
 * `undefined` that silently falls back to localhost in production.
 */
interface ImportMetaEnv {
  /** The Go gateway's REST origin. */
  readonly VITE_SURGE_API_URL?: string;
  /** The gateway's WebSocket endpoint. */
  readonly VITE_SURGE_WS_URL?: string;
  /** The auth service, the only thing the browser asks of Node. */
  readonly VITE_AUTH_BASE_URL?: string;
  /**
   * Stripe's publishable key: public by design, it only lets Stripe.js collect
   * a card for this account. Unset means the payments service runs its fake
   * processor, and the pages save a test card and hide Stripe's own components.
   */
  readonly VITE_STRIPE_PUBLISHABLE_KEY?: string;
  /**
   * Mapbox's public token: it ships in this bundle and signs every tile
   * request, kept safe by its scopes and its URL restriction rather than by
   * being secret. Unset and the map says so instead of drawing nothing.
   */
  readonly VITE_MAPBOX_TOKEN?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
