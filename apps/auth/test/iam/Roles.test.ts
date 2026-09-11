import { changesRole, clampRole, DEFAULT_ROLE } from "@surge/auth/iam/Roles";
import { describe, expect, it } from "vitest";

describe("roles", () => {
  it("lets a new account choose rider or driver, and nothing else", () => {
    expect(clampRole("driver")).toBe("driver");
    expect(clampRole("rider")).toBe("rider");
    // The one that matters: ops is granted, never claimed.
    expect(clampRole("ops")).toBe(DEFAULT_ROLE);
    expect(clampRole(undefined)).toBe(DEFAULT_ROLE);
    expect(clampRole(42)).toBe(DEFAULT_ROLE);
  });

  it("recognises any update that touches the role, whatever it is set to", () => {
    expect(changesRole({ role: "ops" })).toBe(true);
    expect(changesRole({ role: undefined, name: "Ada" })).toBe(true);
    // The updates better-auth makes on its own never carry one.
    expect(changesRole({ emailVerified: true })).toBe(false);
    expect(changesRole({ phoneNumberVerified: true, updatedAt: new Date(0) })).toBe(false);
  });
});
