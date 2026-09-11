import { Alert, AlertDescription } from "@/components/ui/alert.js";
import { errorBody } from "@surge/common/lib/cause";
import { StripeRefused } from "@surge/common/payments/stripe-refused";
import { Cause, Option, Schema } from "effect";
import { TriangleAlert } from "lucide-react";

const isStripeRefused = Schema.is(StripeRefused);

/**
 * Why an action failed, in the server's words when it gave any — or Stripe's,
 * for the writes the browser sends Stripe directly.
 *
 * Anything else — a dropped connection, a defect — is shown through
 * `Cause.pretty`, because the repo rule is that failure UI shows the real cause
 * rather than generic copy. `role="alert"` so a failed tap is announced where
 * it happened, which is what `accessible-notifications-and-messages.md` asks
 * for in place of a toast.
 */
export const ActionError = (props: { readonly cause: Cause.Cause<unknown>; }) => (
  <Alert variant="destructive" role="alert">
    <TriangleAlert className="size-4" aria-hidden />
    <AlertDescription>
      {Option.match(errorBody(props.cause), {
        onSome: (body) =>
          body.error.message,
        onNone: () =>
          Option.match(Option.filter(Cause.findErrorOption(props.cause), isStripeRefused), {
            onSome: (refusal) =>
              refusal.detail,
            onNone: () => (
              <span className="font-mono text-xs whitespace-pre-wrap">
                {Cause.pretty(props.cause)}
              </span>
            ),
          }),
      })}
    </AlertDescription>
  </Alert>
);
