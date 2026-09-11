import { normalizePhoneNumber, OtpCode, PhoneNumber } from "@/iam/auth-schemas.js";
import { Schema } from "effect";
import { describe, expect, it } from "vitest";

const accepts = <S extends Schema.Top>(schema: S, value: unknown) => Schema.is(schema)(value);

describe("sign-in fields", () => {
  it("accepts a number as people type it, and sends it as E.164", () => {
    expect(accepts(PhoneNumber, "+31 6 1234-5678")).toBe(true);
    expect(normalizePhoneNumber("+31 6 1234-5678")).toBe("+31612345678");
  });

  it("asks for the country code rather than guessing one", () => {
    expect(accepts(PhoneNumber, "06 12345678")).toBe(false);
    expect(accepts(PhoneNumber, "+0 6 12345678")).toBe(false);
  });

  it("takes exactly six digits for a code", () => {
    expect(accepts(OtpCode, "123456")).toBe(true);
    expect(accepts(OtpCode, "12345")).toBe(false);
    expect(accepts(OtpCode, "12345a")).toBe(false);
  });
});
