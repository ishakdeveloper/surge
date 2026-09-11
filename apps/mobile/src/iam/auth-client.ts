import { serviceUrls } from "@/lib/config.js";
import { expoClient } from "@better-auth/expo/client";
import {
  emailOTPClient,
  inferAdditionalFields,
  jwtClient,
  phoneNumberClient,
} from "better-auth/client/plugins";
import { createAuthClient } from "better-auth/react";
import * as SecureStore from "expo-secure-store";

/**
 * better-auth, from a phone.
 *
 * A native app has no cookie jar the browser manages for it, so `expoClient`
 * keeps the session cookie in the Keychain / Keystore through SecureStore and
 * puts it on every request to the auth service itself. It also sends
 * `expo-origin`, which the server's `expo()` plugin turns into the `Origin`
 * header better-auth's CSRF check reads — `surge://` in a build, `exp://` under
 * Expo Go — and both are in the auth service's trusted origins.
 *
 * Signing in is a code by email (`emailOtp`) or by text (`phoneNumber`), as on
 * the web; there are no passwords.
 *
 * As on the web, this is the only thing the app asks of Node. Every product API
 * is Go, reached with the JWT that `AuthToken` fetches from `/api/auth/token` —
 * carrying the cookie `getCookie()` reads back out of SecureStore, which is how
 * `packages/client` stays unchanged.
 */
export const authClient = createAuthClient({
  baseURL: serviceUrls.AUTH_BASE_URL,
  plugins: [
    expoClient({ scheme: "surge", storagePrefix: "surge", storage: SecureStore }),
    emailOTPClient(),
    phoneNumberClient(),
    jwtClient(),
    /**
     * `role` is an additional user field on the server, sent with the code that
     * makes an account. The server clamps it regardless, so an account can
     * never make itself `ops` by sending it.
     */
    inferAdditionalFields({ user: { role: { type: "string", required: false, input: true } } }),
  ],
});
