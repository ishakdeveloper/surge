/**
 * Where the browser finds everything, decided when a page is served rather than
 * when the image was built.
 *
 * Vite inlines `import.meta.env.VITE_*` into the client bundle at build time, so
 * an address read that way is fixed per image: staging and production would
 * need two builds, and the image that was tested would not be the image that
 * ships. Instead the server reads its own environment and hands the answer to
 * the browser in the document — `publicConfigScript`, rendered in
 * `routes/__root.tsx`. That inline script sits in `<head>`, so it has run before
 * any of the deferred module scripts that read `publicConfig`.
 *
 * Everything here is sent to every visitor, so it is public by construction:
 * addresses and a publishable key, never a secret.
 *
 * `VITE_*` stays as the fallback, so `pnpm dev` reads `.env` exactly as before.
 */
export interface PublicConfig {
  /** The Go gateway's REST origin. */
  readonly SURGE_API_URL: string | undefined;
  /** The gateway's WebSocket endpoint. */
  readonly SURGE_WS_URL: string | undefined;
  /** The auth service, the only thing the browser asks of Node. */
  readonly AUTH_BASE_URL: string | undefined;
  /** Stripe's publishable key. Unset means the payments service runs its fake. */
  readonly STRIPE_PUBLISHABLE_KEY: string | undefined;
}

declare global {
  interface Window {
    readonly __SURGE_CONFIG__?: PublicConfig;
  }
}

const set = (value: string | undefined): string | undefined => value === "" ? undefined : value;

/** The server's answer: its own environment first, then what Vite loaded. */
export const fromEnvironment = (
  env: Readonly<Record<string, string | undefined>>,
): PublicConfig => ({
  SURGE_API_URL: set(env["SURGE_API_URL"]) ?? import.meta.env.VITE_SURGE_API_URL,
  SURGE_WS_URL: set(env["SURGE_WS_URL"]) ?? import.meta.env.VITE_SURGE_WS_URL,
  AUTH_BASE_URL: set(env["AUTH_BASE_URL"]) ?? import.meta.env.VITE_AUTH_BASE_URL,
  STRIPE_PUBLISHABLE_KEY: set(env["STRIPE_PUBLISHABLE_KEY"])
    ?? import.meta.env.VITE_STRIPE_PUBLISHABLE_KEY,
});

/**
 * This bundle's view. The server reads its environment; the browser reads what
 * the server rendered, and falls back to the build's values only on a page this
 * server did not render.
 */
export const publicConfig: PublicConfig = typeof window === "undefined"
  ? fromEnvironment(process.env)
  : window.__SURGE_CONFIG__ ?? fromEnvironment({});

const prefix = "window.__SURGE_CONFIG__=";

/**
 * The script that hands the server's answer to the browser.
 *
 * JSON is valid JavaScript, but not safe inside a `<script>`: a value holding
 * `</script>` would end the element and let whatever follows run. Escaping every
 * `<` keeps the JSON identical once parsed while giving the HTML parser nothing
 * to close on.
 */
export const publicConfigScript = (config: PublicConfig): string =>
  `${prefix}${JSON.stringify(config).replaceAll("<", "\\u003c")}`;
