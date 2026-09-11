import { Schema } from "effect";

/**
 * Field schemas for signing in, shared by the web app and the phone so a number
 * one accepts is a number the other accepts.
 *
 * Every check carries a `message`, which the default formatter prefers over its
 * generated `Expected a value matching …`.
 */
export const Email = Schema.String.check(
  Schema.isNonEmpty({ message: "Enter your email address." }),
  // Deliberately permissive: the only real proof an address works is that the
  // code sent to it arrives, and over-strict patterns reject valid addresses.
  Schema.isIncludes("@", { message: "That does not look like an email address." }),
);

/**
 * A phone number with its country code.
 *
 * Spaces and dashes are allowed while typing — `+31 6 1234 5678` is how people
 * write one — and `normalizePhoneNumber` removes them before it is sent, since
 * the auth server takes E.164 only. The country code is required rather than
 * guessed: this is one market today, and a guess is what would be wrong on the
 * day it is not.
 */
export const PhoneNumber = Schema.String.check(
  Schema.isNonEmpty({ message: "Enter your phone number." }),
  Schema.isPattern(/^\+[1-9][\d\s-]{6,20}$/, {
    message: "Include the country code, like +31 6 12345678.",
  }),
);

/** `+31 6 1234-5678` → `+31612345678`. */
export const normalizePhoneNumber = (input: string): string => input.replace(/[\s-]/g, "");

/** Checked here so an obviously malformed code never costs one of the server's three attempts. */
export const OtpCode = Schema.String.check(
  Schema.isNonEmpty({ message: "Enter the code we sent you." }),
  Schema.isPattern(/^\d{6}$/, { message: "The code is 6 digits." }),
);

/**
 * What a new account is for, chosen before the code is sent and written when
 * it is verified. Ignored for an account that already exists: a role is chosen
 * once.
 *
 * `ops` is absent on purpose: it is granted, never claimed, and the auth server
 * clamps anything else back to `rider`. Offering it here would be a control that
 * silently does nothing.
 */
export const SignUpRole = Schema.Literals(["rider", "driver"]).annotate({
  identifier: "SignUpRole",
});
export type SignUpRole = typeof SignUpRole.Type;
