import { initialOf, nameOr } from "@surge/common/profile/names";
import { describe, expect, it } from "vitest";

describe("names", () => {
  it("shows the first letter of a name, and none without one", () => {
    expect(initialOf("sanne")).toBe("S");
    expect(initialOf("  Émile")).toBe("É");
    expect(initialOf("   ")).toBeUndefined();
  });

  it("calls someone by their first name, and by their role until they give one", () => {
    expect(nameOr({ displayName: "Sanne" }, "Your driver")).toBe("Sanne");
    expect(nameOr({ displayName: " " }, "Your driver")).toBe("Your driver");
    expect(nameOr(undefined, "Your rider")).toBe("Your rider");
  });
});
