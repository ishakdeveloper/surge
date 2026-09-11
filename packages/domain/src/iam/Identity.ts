import { Schema } from "effect";
import { UserId } from "../api/Primitives.js";

/**
 * Declared beside the API's other branded ids, because chat's responses carry
 * them too. Construct with `UserId.make(value)`, which validates — never cast
 * with `as`.
 */
export { UserId };

/**
 * What a person is to this system. One market, no tenancy, so a role is a
 * fixed union rather than a per-organization string.
 *
 * `ops` is the dispatch console. A driver is also a rider in practice, but the
 * role names which surface they signed up for, not what they are forbidden.
 */
export const Role = Schema.Literals(["rider", "driver", "ops"]).annotate({
  identifier: "Role",
});
export type Role = typeof Role.Type;

/**
 * The authenticated caller.
 *
 * This is the *claims contract*: `apps/auth` mints these fields into a JWT and
 * every Go service parses them back out of one. It is the only shape both
 * languages agree on for identity, so changing it is a cross-language change —
 * `services/pkg/authz` reads the same names.
 */
export class Identity extends Schema.Class<Identity>("Identity")({
  userId: UserId,
  email: Schema.String,
  emailVerified: Schema.Boolean,
  role: Role,
}) {}

/**
 * The authentication failures a caller can act on. Everything else — a broken
 * adapter, a misconfigured secret — is a defect.
 */
export class Unauthenticated extends Schema.TaggedError<Unauthenticated>()("Unauthenticated", {
  reason: Schema.Literals(["NoSession", "SessionExpired"]),
}) {}
