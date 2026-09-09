import { Keys } from "@/atom/reactivity-keys.js";
import { authClient } from "@/iam/auth-client.js";
import { Identity, Unauthenticated } from "@surge/domain/iam/Identity";
import { Effect, Schema } from "effect";
import { Atom } from "effect/unstable/reactivity";

/**
 * Sign-in failures a form can act on. Anything else — a 500, a broken proxy —
 * stays a defect rather than becoming a message the user cannot use.
 */
export class SignInFailed extends Schema.TaggedError<SignInFailed>()("SignInFailed", {
  reason: Schema.Literals(["InvalidCredentials", "RateLimited"]),
}) {}

/**
 * The signed-in identity, or `Unauthenticated` when there is no session.
 *
 * Read from better-auth directly rather than from an API call. There is no
 * Effect server left to ask — every product endpoint is Go, and Go learns who
 * the caller is from the JWT rather than by being asked. So the browser's own
 * notion of "who am I" comes from the same place the cookie does.
 *
 * Decoded through `Identity` rather than trusted: `role` reaches us as a bare
 * string from an `additionalFields` column, and `Identity` is the contract Go
 * parses out of the token. A value that fails here would fail there too, and it
 * is a defect either way — nothing a user can act on.
 */
export const sessionAtom = Atom.withReactivity([Keys.session])(
  Atom.make(
    Effect.gen(function*() {
      const result = yield* Effect.promise(() => authClient.getSession());

      if (result.data == null) {
        return yield* Effect.fail(new Unauthenticated({ reason: "NoSession" }));
      }

      return yield* Schema.decodeUnknownEffect(Identity)({
        userId: result.data.user.id,
        email: result.data.user.email,
        emailVerified: result.data.user.emailVerified,
        role: (result.data.user as Record<string, unknown>)["role"],
      }).pipe(Effect.catchTag("SchemaError", (error) => Effect.die(error)));
    }),
  ),
);

/**
 * better-auth's client resolves with `{ error }` rather than rejecting, so the
 * result is inspected instead of being caught.
 */
const fromAuthResult = (result: { readonly error: { readonly status?: number; } | null; }) =>
  result.error === null
    ? Effect.void
    : new SignInFailed({
      reason: result.error.status === 429 ? "RateLimited" : "InvalidCredentials",
    });

export const signIn = (credentials: { readonly email: string; readonly password: string; }) =>
  Effect.promise(() => authClient.signIn.email(credentials)).pipe(Effect.flatMap(fromAuthResult));

export const signUp = (credentials: { readonly email: string; readonly password: string; }) =>
  Effect.promise(() =>
    authClient.signUp.email({
      ...credentials,
      name: credentials.email.split("@")[0] ?? "You",
    })
  ).pipe(Effect.flatMap(fromAuthResult));

export const signOut = Effect.promise(() => authClient.signOut());

/** Sends the reset link. `redirectTo` is where the emailed link lands. */
export const requestPasswordReset = (email: string) =>
  Effect.promise(() =>
    authClient.requestPasswordReset({
      email,
      redirectTo: `${window.location.origin}/auth/reset-password`,
    })
  ).pipe(Effect.flatMap(fromAuthResult));

export const resetPassword = (options: { readonly token: string; readonly newPassword: string; }) =>
  Effect.promise(() => authClient.resetPassword(options)).pipe(Effect.flatMap(fromAuthResult));

/**
 * Emails a one-time sign-in link. The link lands on better-auth's own verify
 * endpoint, which sets the session cookie and redirects to `callbackURL` — so
 * there is no client route to handle it.
 */
export const sendMagicLink = (email: string) =>
  Effect.promise(() => authClient.signIn.magicLink({ email, callbackURL: window.location.origin }))
    .pipe(Effect.flatMap(fromAuthResult));

/** Emails a six digit code. `sign-in` also creates the user if none exists. */
export const sendOtp = (email: string) =>
  Effect.promise(() => authClient.emailOtp.sendVerificationOtp({ email, type: "sign-in" })).pipe(
    Effect.flatMap(fromAuthResult),
  );

export const verifyOtp = (options: { readonly email: string; readonly otp: string; }) =>
  Effect.promise(() => authClient.signIn.emailOtp(options)).pipe(Effect.flatMap(fromAuthResult));

/**
 * Re-sends the verification email. `callbackURL` is where better-auth's own
 * verify endpoint redirects once the link is followed.
 */
export const resendVerificationEmail = (email: string) =>
  Effect.promise(() =>
    authClient.sendVerificationEmail({
      email,
      callbackURL: `${window.location.origin}/auth/verified`,
    })
  ).pipe(Effect.flatMap(fromAuthResult));

/**
 * Full-page redirect to Google. Nothing resumes after this call, so there is no
 * result to inspect.
 */
export const signInWithGoogle = Effect.promise(() =>
  authClient.signIn.social({ provider: "google", callbackURL: window.location.origin })
);
