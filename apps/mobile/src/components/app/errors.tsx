import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert.js";
import { cn } from "@/lib/utils.js";
import { causeMessage, isWorded } from "@surge/common/lib/cause";
import type { Cause } from "effect";
import type { AsyncResult } from "effect/unstable/reactivity";

/**
 * Why an action failed, in the server's words when it gave any, and otherwise
 * the cause itself — monospaced, because it is a trace rather than a sentence.
 */
export const ActionError = (props: { readonly cause: Cause.Cause<unknown>; }) => (
  <Alert variant="destructive">
    <AlertDescription
      className={cn(
        "text-destructive",
        !isWorded(props.cause) && "font-mono text-xs",
      )}
    >
      {causeMessage(props.cause)}
    </AlertDescription>
  </Alert>
);

/**
 * Why a query did not load.
 *
 * Unlike the web app's, this does not guess at permissions from the shape of
 * the cause. On a phone the usual reason is that the gateway was never reached
 * — a device pointed at `localhost`, a laptop on another network — and that
 * reads as a transport failure in the cause, which is what is shown.
 */
export const QueryError = (props: {
  readonly result: AsyncResult.Failure<unknown, unknown>;
  /** What could not be loaded, lower case: "your trips", "the payment". */
  readonly subject: string;
}) => (
  <Alert variant="destructive">
    <AlertTitle className="text-destructive">Could not load {props.subject}.</AlertTitle>
    <AlertDescription
      className={cn(!isWorded(props.result.cause) && "font-mono text-xs")}
    >
      {causeMessage(props.result.cause)}
    </AlertDescription>
  </Alert>
);
