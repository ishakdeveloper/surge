import { authClient } from "@/iam/auth-client.js";
import { Keys } from "@surge/common/atom/reactivity-keys";
import type { SignUpRole } from "@surge/common/iam/auth-schemas";
import { decodeIdentity, fromAuthResult } from "@surge/common/iam/session";
import type { Contact } from "@surge/domain/iam/Contact";
import { Unauthenticated } from "@surge/domain/iam/Identity";
import { Effect } from "effect";
import { Atom } from "effect/unstable/reactivity";

/**
 * The signed-in identity, or `Unauthenticated` when there is no session.
 *
 * Read from better-auth directly rather than from an API call. There is no
 * Effect server left to ask — every product endpoint is Go, and Go learns who
 * the caller is from the JWT rather than by being asked. So the browser's own
 * notion of "who am I" comes from the same place the cookie does.
 */
export const sessionAtom = Atom.withReactivity([Keys.session])(
  Atom.make(
    Effect.gen(function*() {
      const result = yield* Effect.promise(() => authClient.getSession());

      if (result.data == null) {
        return yield* new Unauthenticated({ reason: "NoSession" });
      }

      return yield* decodeIdentity(result.data.user);
    }),
  ),
);

/**
 * Sends a sign-in code by email or text.
 *
 * There is no separate sign-up: the first code verified for an address or a
 * number makes the account, so this is the same call for a newcomer and for
 * someone coming back.
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
 * already exists.
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

export const signOut = Effect.promise(() => authClient.signOut());

/**
 * Full-page redirect to Google, when the auth server has it configured. Nothing
 * resumes after this call, so there is no result to inspect.
 */
export const signInWithGoogle = Effect.promise(() =>
  authClient.signIn.social({ provider: "google", callbackURL: window.location.origin })
);
