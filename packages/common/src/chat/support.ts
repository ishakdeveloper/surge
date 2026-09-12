import { Schema } from "effect";

/**
 * What a request to support must say, once for every surface that asks: the
 * web's contact form and the phone's check the same limits the chat service
 * enforces.
 */
export const SupportSubject = Schema.String.check(
  Schema.isNonEmpty({ message: "Say in a few words what it is about." }),
  Schema.isMaxLength(120, {
    message: "Keep the subject under 120 characters. The details go below.",
  }),
).annotate({ identifier: "SupportSubject" });

export const SupportBody = Schema.String.check(
  Schema.isNonEmpty({ message: "Tell support what happened." }),
  Schema.isMaxLength(1000, { message: "Keep it under 1,000 characters." }),
).annotate({ identifier: "SupportBody" });
