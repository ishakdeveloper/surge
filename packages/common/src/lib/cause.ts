import type { ErrorCode } from "@surge/domain/api/Primitives";
import { V1ErrorBody } from "@surge/domain/api/SurgeApi";
import { Cause, Option, Schema } from "effect";
import { StripeRefused } from "../payments/stripe-refused.js";

const isErrorBody = Schema.is(V1ErrorBody);
const isStripeRefused = Schema.is(StripeRefused);

/**
 * The gateway's own answer, when the failure carries one.
 *
 * Every status the gateway can send is declared to the generated client, so
 * its `ErrorBody` arrives decoded as the failure and is recognised here by its
 * schema rather than by probing the value for fields.
 */
export const errorBody = (cause: Cause.Cause<unknown>) =>
  Option.filter(Cause.findErrorOption(cause), isErrorBody);

/** Stripe's refusal, for the steps an app takes with Stripe directly. */
export const stripeRefusal = (cause: Cause.Cause<unknown>) =>
  Option.filter(Cause.findErrorOption(cause), isStripeRefused);

/** The stable code a screen can branch on — `failed_precondition` for an expired fare, say. */
export const errorCode = (cause: Cause.Cause<unknown>): Option.Option<ErrorCode> =>
  Option.map(errorBody(cause), (body) => body.error.code);

/**
 * Why something failed, in the server's words when it gave any, or Stripe's.
 *
 * Anything else — above all a connection that never reached the gateway, which
 * on a phone pointed at the wrong host is the common case — is shown through
 * `Cause.pretty`, because failure UI shows the real cause rather than generic
 * copy.
 */
export const causeMessage = (cause: Cause.Cause<unknown>): string =>
  Option.match(errorBody(cause), {
    onSome: (body) => body.error.message,
    onNone: () =>
      Option.match(stripeRefusal(cause), {
        onSome: (refusal) => refusal.detail,
        onNone: () => Cause.pretty(cause),
      }),
  });

/** Whether `causeMessage` is a sentence someone wrote, rather than a trace. */
export const isWorded = (cause: Cause.Cause<unknown>): boolean =>
  Option.isSome(errorBody(cause)) || Option.isSome(stripeRefusal(cause));
