import { Sign, SignGlyph } from "@/components/sign/sign.js";
import { cn } from "@/lib/utils.js";
import { causeMessage, isWorded } from "@surge/common/lib/cause";
import type { AsyncResult } from "effect/unstable/reactivity";
import { TriangleAlert } from "lucide-react";

/**
 * Why a query did not load, on a red sign: what could not be loaded, and the
 * cause itself.
 *
 * The cause is shown rather than guessed at. This used to read every typed
 * failure as "your role does not allow", but a typed failure is any error that
 * reached the channel deliberately — an unreachable service and a missing
 * session are typed too — and a guess about permissions sent people looking in
 * the wrong place. The server's words are shown when it gave any, and the
 * cause's trace otherwise, monospaced because it is a trace rather than a
 * sentence.
 */
export const QueryError = (props: {
  readonly result: AsyncResult.Failure<unknown, unknown>;
  /** What could not be loaded, lower case: "your trips", "the payment". */
  readonly subject: string;
}) => (
  <Sign tone="refused" role="alert" className="items-start">
    <SignGlyph className="pt-0.5">
      <TriangleAlert />
    </SignGlyph>
    <div className="flex min-w-0 flex-1 flex-col gap-1">
      <p className="text-[15px] font-bold">Could not load {props.subject}.</p>
      <p
        className={cn(
          "text-[13px] break-words opacity-90",
          !isWorded(props.result.cause) && "font-mono text-xs",
        )}
      >
        {causeMessage(props.result.cause)}
      </p>
    </div>
  </Sign>
);
