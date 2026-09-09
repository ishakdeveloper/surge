import { emailOTPClient, jwtClient, magicLinkClient } from "better-auth/client/plugins";
import { createAuthClient } from "better-auth/react";

/**
 * Where better-auth answers: the API, always.
 *
 * Every auth route lives on `apps/auth`, so this is simply its address —
 * better-auth's own guidance is to set `baseURL` whenever the auth server is not
 * the app's own origin, and here it never is.
 *
 * This is the *only* thing the browser asks of Node. Every product API is Go,
 * reached with the JWT `jwtClient` fetches from `/api/auth/token`.
 *
 * `VITE_`-prefixed because the browser needs it. That is the only way Vite puts
 * a value in the client bundle, and it is honest about what this is: a public
 * address, not a secret. The same constant serves SSR, which resolves the
 * session in the server bundle.
 */
const baseURL = import.meta.env.VITE_AUTH_BASE_URL ?? "http://localhost:3100";

/**
 * better-auth's own typed client — one of them, used from both sides.
 *
 * The available methods are inferred from the plugin set, so sign-in, magic
 * link and OTP are reached as `authClient.signIn.email(...)` rather than by
 * writing endpoint paths by hand. It also owns the session cookie, which is why
 * authentication stays here rather than moving to Go with everything else:
 * a cookie is set by an HTTP response header, and better-auth owns that.
 *
 * The server uses it too, for the `/_protected` guard — passing the incoming
 * request's cookie per call, since there is no ambient one to pick up. A second
 * client for that would be the same configuration twice, free to drift.
 *
 * `jwtClient` is what bridges to Go: it exchanges the session cookie for a
 * short-lived EdDSA token that every Go service verifies locally against
 * `/api/auth/jwks`, so no request path touches this service or its database.
 */
export const authClient = createAuthClient({
  baseURL,
  plugins: [magicLinkClient(), emailOTPClient(), jwtClient()],
});
