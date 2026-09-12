import { compileAuthMigrations } from "@surge/auth/iam/Options";
import { testDbUrl } from "@surge/database/PgTest";
import { Pool } from "pg";
import { afterAll, describe, expect, it } from "vitest";

/**
 * `0001_auth.sql` was generated from the plugin set, and later migrations extend
 * it by hand. Nothing regenerates it on every change — the generator diffs
 * against a live database, so its output depends on what that database already
 * holds — which leaves this as what keeps the two in agreement.
 *
 * Global setup has applied every committed migration. Asked what it would still
 * create, better-auth must answer nothing: anything at all is a plugin added or
 * changed without the migration to match, and a deploy that would fail at the
 * first sign-in rather than in review.
 */
const url = testDbUrl();
const pool = url === undefined ? undefined : new Pool({ connectionString: url });

afterAll(async () => {
  await pool?.end();
});

describe.skipIf(pool === undefined)("the committed auth migrations", () => {
  it("leave better-auth nothing to create", async () => {
    const pending = await compileAuthMigrations({
      pool: pool!,
      baseURL: "http://localhost:3200",
      secret: "a-test-secret-that-is-comfortably-long-enough",
      trustedOrigins: ["http://localhost:5273"],
      google: undefined,
      cookieDomain: undefined,
      sendEmail: async () => {},
      sendSms: async () => {},
      smsDomain: "localhost:5273",
    });

    // The compiler joins statements with `;` and always ends with one, so no
    // work at all compiles to a lone semicolon.
    expect(pending.replaceAll(";", "").trim()).toBe("");
  });
});
