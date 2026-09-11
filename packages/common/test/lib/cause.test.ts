import { causeMessage, errorCode } from "@/lib/cause.js";
import { Cause, Option } from "effect";
import { describe, expect, it } from "vitest";

describe("failure messages", () => {
  it("uses the gateway's own words and code when it answered", () => {
    const cause = Cause.fail({ error: { code: "failed_precondition", message: "fare expired" } });

    expect(errorCode(cause)).toEqual(Option.some("failed_precondition"));
    expect(causeMessage(cause)).toBe("fare expired");
  });

  it("shows the cause itself when the gateway was never reached", () => {
    // What a phone pointed at the wrong host produces: no ErrorBody, and a
    // message that has to say what actually happened.
    const cause = Cause.die("connection refused");

    expect(Option.isNone(errorCode(cause))).toBe(true);
    expect(causeMessage(cause)).toContain("connection refused");
  });
});
