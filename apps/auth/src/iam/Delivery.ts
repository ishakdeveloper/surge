import { APIError } from "better-auth/api";
import { Cause, Effect, Exit, Option } from "effect";
import type { EmailSendFailed } from "../email/Mailer.js";
import type { SmsSendFailed } from "../sms/Sms.js";

/**
 * How a failed send reaches the person who asked for a code.
 *
 * better-auth runs the senders inside its own request, and a rejected promise
 * there becomes a 500 — which the apps rightly word as the service failing.
 * Some failures are not that: a number that cannot take a text, too many codes
 * too quickly. Those are thrown as better-auth's own `APIError`, which it
 * answers with that status, and which the apps word as something the person
 * can act on.
 *
 * That holds for texts. better-auth catches and logs whatever the email sender
 * throws, so asking for an email code answers the same whether one went out or
 * not — which keeps the endpoint from telling anyone which addresses a provider
 * accepts. An email refusal ends in the log.
 */
const refusal = (error: EmailSendFailed | SmsSendFailed): APIError => {
  switch (error.reason) {
    case "RateLimited":
      return new APIError("TOO_MANY_REQUESTS", {
        message: "Too many codes. Try again in a minute.",
      });
    case "InvalidRecipient":
    case "Undeliverable":
      return new APIError("BAD_REQUEST", { message: "We could not send a code there." });
    case "Unavailable":
      return new APIError("SERVICE_UNAVAILABLE", { message: "Codes cannot be sent right now." });
  }
};

/**
 * Runs a send for better-auth: done, a refusal it can answer with a status, or
 * — for anything else — the defect as it was, which becomes its 500.
 */
export const deliver = async (
  send: Effect.Effect<void, EmailSendFailed | SmsSendFailed>,
): Promise<void> => {
  const exit = await Effect.runPromiseExit(send);
  if (Exit.isSuccess(exit)) return;

  const failure = Cause.findErrorOption(exit.cause);
  throw Option.isSome(failure) ? refusal(failure.value) : Cause.squash(exit.cause);
};
