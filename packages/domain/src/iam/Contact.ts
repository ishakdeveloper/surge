/**
 * How a person signs in: an email address or a phone number, each proven by a
 * one-time code.
 *
 * better-auth's user table requires an email, so an account made from a phone
 * number is given a placeholder one, derived from the number, on `.invalid` —
 * a top-level domain reserved by RFC 2606 so that nothing sent to it can ever
 * be delivered. That keeps the `Identity` contract Go parses out of the token
 * unchanged, and this module is the one place that knows the convention: the
 * auth server writes it, and both apps read the number back out for display.
 */
export const PHONE_ACCOUNT_DOMAIN = "phone.surge.invalid";

/** `+31612345678` → `31612345678@phone.surge.invalid`. */
export const phoneAccountEmail = (phoneNumber: string): string =>
  `${phoneNumber.replace(/^\+/, "")}@${PHONE_ACCOUNT_DOMAIN}`;

export type Contact =
  | { readonly kind: "email"; readonly email: string; }
  | { readonly kind: "phone"; readonly phoneNumber: string; };

/** What an account's email says about how its owner signs in. */
export const contactOf = (email: string): Contact => {
  const suffix = `@${PHONE_ACCOUNT_DOMAIN}`;
  return email.endsWith(suffix)
    ? { kind: "phone", phoneNumber: `+${email.slice(0, -suffix.length)}` }
    : { kind: "email", email };
};

/** The address or number, as a person would recognise it. */
export const describeContact = (contact: Contact): string =>
  contact.kind === "email" ? contact.email : contact.phoneNumber;
