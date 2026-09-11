import { ActionError } from "@/components/app/action-error.js";
import { Alert, AlertDescription } from "@/components/ui/alert.js";
import { Badge } from "@/components/ui/badge.js";
import { Button } from "@/components/ui/button.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { PositionUnavailable } from "@surge/client/Geolocation";
import { locateMe, shiftAtom } from "@surge/common/atom/driver-atoms";
import { connectionAtom } from "@surge/common/atom/realtime-atoms";
import { activeTripAtom } from "@surge/common/atom/trip-atoms";
import { statusFor } from "@surge/common/drive/driver-status";
import { formatPoint } from "@surge/common/lib/format";
import { Cause, Option, Schema } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";

const isPositionUnavailable = Schema.is(PositionUnavailable);

/** What each refusal means for the person holding the phone. */
const positionMessage: Record<PositionUnavailable["reason"], string> = {
  Unsupported: "This browser cannot report a position. Click the map to place yourself.",
  Denied: "Location permission was denied. Click the map to place yourself instead.",
  Unavailable: "No position is available right now. Click the map to place yourself.",
  Timeout: "Finding your position took too long. Click the map to place yourself.",
};

/**
 * The shift: connection, position, and whether the driver wants work.
 *
 * The status sent to the matcher is shown as it is sent — `idle`, `offline`,
 * `enroute_pickup` — because it is derived, not chosen, and a driver wondering
 * why no offers arrive should be able to see what the system thinks they are.
 */
export const ShiftPanel = () => {
  const shift = useAtomValue(shiftAtom);
  const setShift = useAtomSet(shiftAtom);
  const connection = useAtomValue(connectionAtom);
  const locating = useAtomValue(locateMe);
  const locate = useAtomSet(locateMe);
  const tripStatus = useAtomValue(
    activeTripAtom,
    (result) => Option.map(Option.flatten(AsyncResult.value(result)), (trip) => trip.status),
  );

  const connected = AsyncResult.isSuccess(connection) && connection.value === "Connected";
  const refusal = AsyncResult.isFailure(locating)
    ? Option.filter(Cause.findErrorOption(locating.cause), isPositionUnavailable)
    : Option.none();

  return (
    <section
      className="flex flex-col gap-3 rounded-md border border-border p-4"
      aria-labelledby="shift"
    >
      <div className="flex items-center justify-between">
        <h2 id="shift" className="text-sm font-medium">Shift</h2>
        <Badge variant={connected ? "default" : "outline"}>
          {connected ? "Connected" : "Connecting…"}
        </Badge>
      </div>

      <dl className="grid grid-cols-[5rem_1fr] gap-1.5 text-sm">
        <dt className="text-muted-foreground">Position</dt>
        <dd className="font-mono text-xs">
          {Option.match(shift.position, { onNone: () => "-", onSome: formatPoint })}
        </dd>
        <dt className="text-muted-foreground">Status</dt>
        <dd className="font-mono text-xs">{statusFor(shift.online, tripStatus)}</dd>
      </dl>

      {Option.match(refusal, {
        onSome: (error) => (
          <Alert role="alert">
            <AlertDescription>{positionMessage[error.reason]}</AlertDescription>
          </Alert>
        ),
        onNone: () =>
          AsyncResult.isFailure(locating) ? <ActionError cause={locating.cause} /> : null,
      })}

      <div className="flex gap-2">
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={locating.waiting}
          onClick={() => {
            locate();
          }}
        >
          {locating.waiting ? "Locating…" : "Use my location"}
        </Button>
        <Button
          type="button"
          size="sm"
          variant={shift.online ? "outline" : "default"}
          disabled={Option.isNone(shift.position)}
          onClick={() => {
            setShift({ ...shift, online: !shift.online });
          }}
        >
          {shift.online ? "Go offline" : "Go online"}
        </Button>
      </div>
    </section>
  );
};
