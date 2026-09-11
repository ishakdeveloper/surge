import { Identity } from "@surge/domain/iam/Identity";
import { Effect, Schema } from "effect";

/**
 * The parts of a session both apps treat the same way. Each app keeps its own
 * better-auth client — cookies in the browser, SecureStore on the phone — and
 * its own session atom over it; what the client *returns* is read here.
 */

/**
 * Sign-in failures a form can act on: a code that is wrong, expired or used up,
 * and too many attempts. Anything else — a 500, a broken proxy — stays a defect
 * rather than becoming a message the user cannot use.
 */
export class SignInFailed extends Schema.TaggedError<SignInFailed>()("SignInFailed", {
  reason: Schema.Literals(["InvalidCode", "RateLimited"]),
}) {}

/**
 * better-auth's client resolves with `{ error }` rather than rejecting, so the
 * result is inspected instead of being caught. A refusal — a 4xx — is the
 * person's to act on. Anything else is the service failing, and telling them
 * their code was wrong would send them looking in the wrong place.
 */
export const fromAuthResult = (
  result: {
    readonly error: {
      readonly status?: number | undefined;
      readonly message?: string | undefined;
    } | null;
  },
): Effect.Effect<void, SignInFailed> => {
  if (result.error === null) return Effect.void;
  const status = result.error.status ?? 0;
  if (status === 429) return Effect.fail(new SignInFailed({ reason: "RateLimited" }));
  if (status >= 400 && status < 500) {
    return Effect.fail(new SignInFailed({ reason: "InvalidCode" }));
  }
  return Effect.die(
    new Error(`The auth service failed (${status}): ${result.error.message ?? "no message"}`),
  );
};

/**
 * better-auth's user, as the `Identity` Go will parse out of the token.
 *
 * Decoded rather than trusted: `role` reaches the client as a bare string from
 * an `additionalFields` column, and `Identity` is the contract. A value that
 * fails here would fail in Go too, and it is a defect either way — nothing a
 * user can act on.
 */
export const decodeIdentity = (user: {
  readonly id: string;
  readonly email: string;
  readonly emailVerified: boolean;
  readonly role?: unknown;
}) =>
  Schema.decodeUnknownEffect(Identity)({
    userId: user.id,
    email: user.email,
    emailVerified: user.emailVerified,
    role: user.role,
  }).pipe(Effect.catchTag("SchemaError", (error) => Effect.die(error)));
