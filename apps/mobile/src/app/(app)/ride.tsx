import { QueryError } from "@/components/app/errors.js";
import { Screen } from "@/components/app/screen.js";
import { Text } from "@/components/ui/text.js";
import { BookRide } from "@/ride/book-ride.js";
import { RiderTrip } from "@/ride/rider-trip.js";
import { useAtomMount, useAtomValue } from "@effect/atom-react";
import { paymentPushesAtom } from "@surge/common/atom/payment-atoms";
import { activeTripAtom, tripPushesAtom } from "@surge/common/atom/trip-atoms";
import { Option } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";

/**
 * The rider's surface: book a trip, then watch it happen. Nothing polls — the
 * two push bridges, mounted for as long as this tab is, turn server pushes
 * into refreshed queries.
 */
const Ride = () => {
  useAtomMount(tripPushesAtom);
  useAtomMount(paymentPushesAtom);
  const active = useAtomValue(activeTripAtom);

  if (AsyncResult.isInitial(active)) {
    return (
      <Screen>
        <Text className="text-muted-foreground">Loading your trips…</Text>
      </Screen>
    );
  }
  if (AsyncResult.isFailure(active)) {
    return (
      <Screen>
        <QueryError result={active} subject="your trips" />
      </Screen>
    );
  }

  return Option.isSome(active.value) ? <RiderTrip /> : <BookRide />;
};

export default Ride;
