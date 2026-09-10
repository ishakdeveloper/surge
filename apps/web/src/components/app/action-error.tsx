import { Alert, AlertDescription } from "@/components/ui/alert.js";
import type { ErrorCode } from "@surge/domain/api/Primitives";
import { V1ErrorBody } from "@surge/domain/api/SurgeApi";
import { Cause, Option, Schema } from "effect";
import { TriangleAlert } from "lucide-react";

const isErrorBody = Schema.is(V1ErrorBody);

/**
 * The gateway's own answer, when the failure carries one.
 *
 * Every status the gateway can send is declared to the generated client, so its
 * `ErrorBody` arrives decoded as the failure. It is recognised here by its
 * schema rather than by probing the value for fields.
 */
export const errorBody = (cause: Cause.Cause<unknown>) =>
  Option.filter(Cause.findErrorOption(cause), isErrorBody);

/** The stable code a page can branch on — `failed_precondition` for an expired fare, say. */
export const errorCode = (cause: Cause.Cause<unknown>): Option.Option<ErrorCode> =>
  Option.map(errorBody(cause), (body) => body.error.code);

/**
 * Why an action failed, in the server's words when it gave any.
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
        onNone: () => (
          <span className="font-mono text-xs whitespace-pre-wrap">{Cause.pretty(props.cause)}</span>
        ),
      })}
    </AlertDescription>
  </Alert>
);
