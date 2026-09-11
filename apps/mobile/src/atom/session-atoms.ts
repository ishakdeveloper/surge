import { authClient } from "@/iam/auth-client.js";
import { runtime } from "@surge/common/atom/runtime";
import type { SignUpRole } from "@surge/common/iam/auth-schemas";
import { decodeIdentity, fromAuthResult } from "@surge/common/iam/session";
import type { Contact } from "@surge/domain/iam/Contact";
import { Unauthenticated } from "@surge/domain/iam/Identity";
import { Effect } from "effect";
import { Atom } from "effect/unstable/reactivity";

/**
 * The signed-in identity, or `Unauthenticated` when there is no session — the
 * web app's session atom, over the Expo client.
 *
 * Read once per registry. A new session means a new registry — see
 * `iam/session-scope.tsx` — so there is nothing to invalidate.
 */
export const sessionAtom = Atom.make(
  Effect.gen(function*() {
    const result = yield* Effect.promise(() => authClient.getSession());

    if (result.data == null) {
      return yield* new Unauthenticated({ reason: "NoSession" });
    }

    return yield* decodeIdentity(result.data.user);
  }),
);

/**
 * Sends a sign-in code by email or text. The first code verified for an address
 * or a number makes the account, so this is the same call for a newcomer.
 */
export const sendCode = (to: Contact) =>
  (to.kind === "email"
    ? Effect.promise(() =>
      authClient.emailOtp.sendVerificationOtp({ email: to.email, type: "sign-in" })
    )
    : Effect.promise(() => authClient.phoneNumber.sendOtp({ phoneNumber: to.phoneNumber })))
    .pipe(Effect.flatMap(fromAuthResult));

/**
 * Verifies a code, which signs in — and makes the account the first time, with
 * `role`. The auth server clamps the role, and ignores it for an account that
 * already exists. The cookie is in SecureStore once this succeeds.
 */
export const verifyCode = (request: {
  readonly to: Contact;
  readonly code: string;
  readonly role: SignUpRole;
}) => {
  const { to, code, role } = request;
  return (to.kind === "email"
    ? Effect.promise(() => authClient.signIn.emailOtp({ email: to.email, otp: code, role }))
    : Effect.promise(() =>
      authClient.phoneNumber.verify({ phoneNumber: to.phoneNumber, code, role })
    ))
    .pipe(Effect.flatMap(fromAuthResult));
};

/** Clears the cookie SecureStore holds. The caller restarts the session scope once it lands. */
export const signOut = runtime.fn((_: void) =>
  Effect.promise(() => authClient.signOut()).pipe(Effect.asVoid)
);
