import { fromAuthResult, SignInFailed } from "@/iam/session.js";
import { Cause, Effect, Exit } from "effect";
import { describe, expect, it } from "vitest";

const outcome = (error: { readonly status?: number; readonly message?: string; } | null) =>
  Effect.runSyncExit(fromAuthResult({ error }));

/**
 * What better-auth's answer becomes. The line that matters is between the
 * person's mistake and the service's: a refusal is a message they can act on,
 * and a 500 must not be dressed up as "your code was wrong".
 */
describe("reading better-auth's answer", () => {
  it("succeeds when there is no error", () => {
    expect(Exit.isSuccess(outcome(null))).toBe(true);
  });

  it("reads a 429 as too many attempts", () => {
    expect(outcome({ status: 429 })).toEqual(
      Exit.fail(new SignInFailed({ reason: "RateLimited" })),
    );
  });

  it("reads any other refusal as a code that will not do", () => {
    for (const status of [400, 401, 403]) {
      expect(outcome({ status })).toEqual(Exit.fail(new SignInFailed({ reason: "InvalidCode" })));
    }
  });

  it("reads a server error, or no answer at all, as the service failing", () => {
    for (const error of [{ status: 500, message: "column does not exist" }, {}]) {
      const exit = outcome(error);

      expect(Exit.isFailure(exit) && Cause.hasDies(exit.cause)).toBe(true);
    }
  });
});
