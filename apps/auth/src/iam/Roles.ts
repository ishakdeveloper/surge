/**
 * Who may be what.
 *
 * `role` is a better-auth additional field marked `input: true`, because a
 * driver has to be able to sign up as one — and `input: true` means better-auth
 * accepts it from the client *everywhere* it parses user input, which includes
 * `/update-user` as well as account creation. So it is guarded at both doors:
 * chosen once, from a fixed set, when the account is made, and never changed by
 * a client afterwards. `ops` is granted by an operator against the database
 * (`make grant-ops`), and nothing a client sends can reach it.
 */

/** The roles a caller may assign themselves. `ops` is granted, never claimed. */
const SELF_ASSIGNABLE_ROLES: ReadonlySet<string> = new Set(["rider", "driver"]);

export const DEFAULT_ROLE = "rider";

/** At creation: anything outside the self-assignable set becomes the default. */
export const clampRole = (requested: unknown): string =>
  typeof requested === "string" && SELF_ASSIGNABLE_ROLES.has(requested)
    ? requested
    : DEFAULT_ROLE;

/**
 * On update: whether the change touches the role at all — which is refused
 * outright rather than filtered.
 *
 * Refused, not stripped, because better-auth *merges* an update hook's result
 * into the original change: a hook that returns the change without `role` still
 * writes the role. The only answers that hold are rejecting the request and
 * cancelling the write, and `Options.ts` does both.
 */
export const changesRole = (update: Readonly<Record<string, unknown>>): boolean =>
  Object.hasOwn(update, "role");
