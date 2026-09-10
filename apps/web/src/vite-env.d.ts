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
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
