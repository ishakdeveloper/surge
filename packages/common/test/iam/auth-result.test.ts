import { submitMessage } from "@/iam/auth-result.js";
import { SignInFailed } from "@/iam/session.js";
import { Cause } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";
import { describe, expect, it } from "vitest";

const REJECTED = "That code is wrong or has expired.";

const failedWith = (cause: Cause.Cause<{ readonly _tag: string; readonly reason?: string; }>) =>
  submitMessage(AsyncResult.failure(cause), REJECTED);

/**
 * A failed submit has four different causes, and each sends the person
 * somewhere different — the fields, the clock, the code, or nowhere at all.
 */
describe("the message for a failed submit", () => {
  it("points at the fields when the form did not validate", () => {
    expect(failedWith(Cause.fail({ _tag: "SchemaError" }))).toBe(
      "Check the fields above and try again.",
    );
  });

  it("asks for patience when there were too many attempts", () => {
    expect(failedWith(Cause.fail(new SignInFailed({ reason: "RateLimited" })))).toBe(
      "Too many attempts. Try again in a minute.",
    );
  });

  it("gives the form's own words when the service refused the request", () => {
    expect(failedWith(Cause.fail(new SignInFailed({ reason: "InvalidCode" })))).toBe(REJECTED);
  });

  it("takes the blame when the service broke", () => {
    expect(failedWith(Cause.die(new Error("column does not exist")))).toBe(
      "Something went wrong on our side. Try again shortly.",
    );
  });
});
