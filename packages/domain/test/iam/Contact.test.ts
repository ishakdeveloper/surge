import { contactOf, describeContact, phoneAccountEmail } from "@surge/domain/iam/Contact";
import { describe, expect, it } from "vitest";

describe("sign-in contact", () => {
  it("round-trips a phone number through its placeholder email", () => {
    const email = phoneAccountEmail("+31612345678");

    expect(email).toBe("31612345678@phone.surge.invalid");
    expect(contactOf(email)).toEqual({ kind: "phone", phoneNumber: "+31612345678" });
  });

  it("leaves a real email as an email", () => {
    const contact = contactOf("ada@example.com");

    expect(contact).toEqual({ kind: "email", email: "ada@example.com" });
    expect(describeContact(contact)).toBe("ada@example.com");
  });

  it("does not mistake an address that merely mentions the domain", () => {
    expect(contactOf("phone.surge.invalid@example.com").kind).toBe("email");
  });
});
