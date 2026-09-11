import { Announced } from "@/components/app/announced.js";
import { Detail, Details } from "@/components/app/details.js";
import { ActionError } from "@/components/app/errors.js";
import { Button } from "@/components/ui/button.js";
import { Heading, Text } from "@/components/ui/text.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { activeTripAtom, arriveTrip, completeTrip, startTrip } from "@surge/common/atom/trip-atoms";
import {
  driverStatus,
  formatCents,
  formatDistance,
  formatDuration,
  formatPoint,
} from "@surge/common/lib/format";
import { Option } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";
import { View } from "react-native";

/**
 * The trip a driver is on, and the one thing to do next. The trip service
 * refuses the steps out of order, so showing all three would be two buttons
 * that can only fail.
 */
export const DriverTrip = () => {
  const active = useAtomValue(activeTripAtom).pipe(AsyncResult.getOrThrow);
  const arriving = useAtomValue(arriveTrip);
  const arrive = useAtomSet(arriveTrip);
  const starting = useAtomValue(startTrip);
  const start = useAtomSet(startTrip);
  const completing = useAtomValue(completeTrip);
  const complete = useAtomSet(completeTrip);

  if (Option.isNone(active)) return null;
  const trip = active.value;

  const step = trip.status === "TRIP_STATUS_ACCEPTED"
    ? {
      label: "I have arrived",
      busy: arriving,
      run: () => {
        arrive(trip.id);
      },
    }
    : trip.status === "TRIP_STATUS_ARRIVED"
    ? {
      label: "Start the trip",
      busy: starting,
      run: () => {
        start(trip.id);
      },
    }
    : trip.status === "TRIP_STATUS_IN_PROGRESS"
    ? {
      label: "Complete the trip",
      busy: completing,
      run: () => {
        complete(trip.id);
      },
    }
    : undefined;

  return (
    <View className="gap-3">
      <Heading>Your trip</Heading>

      <Announced
        message={driverStatus[trip.status]}
        className="gap-1 rounded-xl border border-border bg-card p-4"
      >
        <Text className="text-base font-medium">{driverStatus[trip.status]}</Text>
        <Text className="font-mono text-xs text-muted-foreground">{trip.status}</Text>
      </Announced>

      <Details>
        <Detail label="Trip" value={trip.id} mono />
        <Detail label="Rider" value={trip.riderId} mono />
        <Detail label="Pickup" value={formatPoint(trip.pickup)} mono />
        <Detail label="Dropoff" value={formatPoint(trip.dropoff)} mono />
        <Detail label="Fare" value={formatCents(trip.totalCents)} />
        <Detail
          label="Route"
          value={`${formatDistance(trip.route.meters)} · about ${
            formatDuration(trip.route.seconds)
          }`}
        />
      </Details>

      {step !== undefined && (
        <>
          {AsyncResult.isFailure(step.busy) && <ActionError cause={step.busy.cause} />}
          <Button disabled={step.busy.waiting} onPress={step.run}>
            {step.busy.waiting ? "Saving…" : step.label}
          </Button>
        </>
      )}
    </View>
  );
};
