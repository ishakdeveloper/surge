import { publicConfig } from "@/lib/public-config.js";
import {
  emailOTPClient,
  inferAdditionalFields,
  jwtClient,
  phoneNumberClient,
} from "better-auth/client/plugins";
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
 * The public address, from `publicConfig`: what the server was told at runtime,
 * not what the image was built with.
 */
export const authBaseUrl = publicConfig.AUTH_BASE_URL ?? "http://localhost:3200";

/**
 * Where this bundle reaches it.
 *
 * The browser uses the public address. The server, resolving a session while it
 * renders, uses `AUTH_INTERNAL_URL` when it has one, so that request stays
 * inside the cluster instead of leaving through the load balancer and coming
 * back in.
 */
const internalUrl = typeof window === "undefined" ? process.env["AUTH_INTERNAL_URL"] : undefined;
const baseURL = internalUrl === undefined || internalUrl === "" ? authBaseUrl : internalUrl;

/**
 * better-auth's own typed client — one of them, used from both sides.
 *
 * The available methods are inferred from the plugin set: a code by email
 * (`emailOtp`) or by text (`phoneNumber`), and nothing with a password. It also
 * owns the session cookie, which is why authentication stays here rather than
 * moving to Go with everything else: a cookie is set by an HTTP response header,
 * and better-auth owns that.
 *
 * The server uses it too, for the `/_protected` guard — passing the incoming
 * request's cookie per call, since there is no ambient one to pick up.
 *
 * `jwtClient` is what bridges to Go: it exchanges the session cookie for a
 * short-lived EdDSA token that every Go service verifies locally against
 * `/api/auth/jwks`, so no request path touches this service or its database.
 */
export const authClient = createAuthClient({
  baseURL,
  plugins: [
    emailOTPClient(),
    phoneNumberClient(),
    jwtClient(),
    /**
     * `role` is an additional user field on the server, sent with the code that
     * makes an account: riders and drivers choose on the way in. Declaring it
     * here is what types that argument — the server clamps the value
     * regardless, so an account can never make itself `ops` by sending it.
     */
    inferAdditionalFields({ user: { role: { type: "string", required: false, input: true } } }),
  ],
});
