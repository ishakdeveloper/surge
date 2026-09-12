import { CounterpartCard } from "@/chat/counterpart-card.js";
import { Announced } from "@/components/app/announced.js";
import { ActionError } from "@/components/app/errors.js";
import { IconBubble, Sign, SignText, type Tone } from "@/components/sign/sign.js";
import { Button } from "@/components/ui/button.js";
import { Text } from "@/components/ui/text.js";
import { type Stop, StopRow, StopsCard } from "@/ride/place-field.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { activeTripAtom, arriveTrip, completeTrip, startTrip } from "@surge/common/atom/trip-atoms";
import {
  driverStatus,
  formatCents,
  formatDistance,
  formatDuration,
} from "@surge/common/lib/format";
import { UserId } from "@surge/domain/api/Primitives";
import type { Trip } from "@surge/domain/trip/Trip";
import { Option } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";
import { View } from "react-native";
import Animated, { FadeInDown, ReduceMotion } from "react-native-reanimated";

/** What the driver should be doing now, under the status. */
const NEXT: Partial<Record<Trip["status"], string>> = {
  TRIP_STATUS_ACCEPTED: "Head to the pickup.",
  TRIP_STATUS_ARRIVED: "Your rider is on their way out to you.",
  TRIP_STATUS_IN_PROGRESS: "Take them to the dropoff.",
};

const toneOf = (status: Trip["status"]): Tone =>
  status === "TRIP_STATUS_ARRIVED" ? "done" : "direction";

/**
 * The trip a driver is on, as the sheet under the map shows it — the rider's
 * sheet from the other side: what is happening now, the rider as a person
 * with a way to message them, the stops and the fare. The one step to take
 * next is `DriverTripStep`, pinned under the thumb.
 */
export const DriverTrip = () => {
  const active = useAtomValue(activeTripAtom).pipe(AsyncResult.getOrThrow);
  if (Option.isNone(active)) return null;
  const trip = active.value;
  const pickup: Stop = { position: trip.pickup, place: Option.none() };
  const dropoff: Stop = { position: trip.dropoff, place: Option.none() };

  return (
    <View className="gap-2">
      {/* Announced as it changes: each step is news to the driver as well. */}
      <Announced message={driverStatus[trip.status]} className="">
        <Animated.View
          key={trip.status}
          entering={FadeInDown.duration(240).reduceMotion(ReduceMotion.System)}
        >
          <Sign tone={toneOf(trip.status)} className="py-4">
            <IconBubble
              name={trip.status === "TRIP_STATUS_ARRIVED"
                ? "checkmark"
                : trip.status === "TRIP_STATUS_IN_PROGRESS"
                ? "flag-outline"
                : "navigate-outline"}
            />
            <View className="min-w-0 flex-1 gap-0.5">
              <SignText className="text-xl leading-tight font-semibold">
                {driverStatus[trip.status]}
              </SignText>
              {NEXT[trip.status] !== undefined && (
                <SignText className="text-sm opacity-80">{NEXT[trip.status]}</SignText>
              )}
            </View>
          </Sign>
        </Animated.View>
      </Announced>

      {trip.riderId !== "" && (
        <CounterpartCard
          tripId={trip.id}
          userId={UserId.make(trip.riderId)}
          relation="Your rider"
          detail={trip.status === "TRIP_STATUS_IN_PROGRESS" ? "In your car" : "Waiting for you"}
        />
      )}

      <StopsCard
        rail
        footer={
          <Text className="px-3 pt-0.5 pb-1.5 text-right text-sm font-semibold tabular-nums">
            {formatDuration(trip.route.seconds)} · {formatDistance(trip.route.meters)}
          </Text>
        }
      >
        <StopRow kind="pickup" label="Pickup" stop={pickup} />
        <StopRow kind="dropoff" label="Dropoff" stop={dropoff} />
      </StopsCard>

      <Sign>
        <IconBubble name="cash-outline" />
        <SignText className="flex-1 text-base font-semibold">Fare</SignText>
        <SignText className="text-[22px] font-semibold tabular-nums">
          {formatCents(trip.totalCents)}
        </SignText>
      </Sign>

      <Text className="px-1 pt-1 text-center text-xs text-muted-foreground">
        Trip <Text className="font-mono text-xs text-muted-foreground">{trip.id}</Text>
      </Text>
    </View>
  );
};

/**
 * The one thing to do next, as the sheet's pinned action. The trip service
 * refuses the steps out of order, so showing all three would be two buttons
 * that can only fail.
 */
export const DriverTripStep = () => {
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
  if (step === undefined) return null;

  return (
    <View className="gap-2">
      {AsyncResult.isFailure(step.busy) && <ActionError cause={step.busy.cause} />}
      <Button
        className="h-14"
        disabled={step.busy.waiting}
        onPress={() => {
          step.run();
        }}
      >
        {step.busy.waiting ? "Saving…" : step.label}
      </Button>
    </View>
  );
};
