import { Sign } from "@/components/sign/sign.js";
import { errorBody } from "@surge/common/lib/cause";
import { StripeRefused } from "@surge/common/payments/stripe-refused";
import { Cause, Option, Schema } from "effect";
import { TriangleAlert } from "lucide-react";

const isStripeRefused = Schema.is(StripeRefused);

/**
 * Why an action failed, on the refused red card, in the server's words when it
 * gave any — or Stripe's, for the writes the browser sends Stripe directly.
 *
 * Anything else — a dropped connection, a defect — is shown through
 * `Cause.pretty`, because the repo rule is that failure UI shows the real cause
 * rather than generic copy. `role="alert"` so a failed tap is announced where
 * it happened, which is what `accessible-notifications-and-messages.md` asks
 * for in place of a toast.
 */
export const ActionError = (props: { readonly cause: Cause.Cause<unknown>; }) => (
  <Sign tone="refused" role="alert" className="items-start motion-safe:animate-rise-in">
    <TriangleAlert aria-hidden className="mt-0.5 size-5 shrink-0" />
    <p className="min-w-0 flex-1 text-[15px] font-semibold break-words">
      {Option.match(errorBody(props.cause), {
        onSome: (body) =>
          body.error.message,
        onNone: () =>
          Option.match(Option.filter(Cause.findErrorOption(props.cause), isStripeRefused), {
            onSome: (refusal) =>
              refusal.detail,
            onNone: () => (
              <span className="font-mono text-xs font-normal whitespace-pre-wrap">
                {Cause.pretty(props.cause)}
              </span>
            ),
          }),
      })}
    </p>
  </Sign>
);
