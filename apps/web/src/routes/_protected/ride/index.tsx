import { paymentPushesAtom } from "@/atom/payment-atoms.js";
import { sessionAtom } from "@/atom/session-atoms.js";
import { activeTripAtom, tripPushesAtom } from "@/atom/trip-atoms.js";
import { QueryError } from "@/components/app/query-error.js";
import { BookRide } from "@/routes/_protected/ride/-components/book-ride.js";
import { RiderTrip } from "@/routes/_protected/ride/-components/rider-trip.js";
import { useAtomMount, useAtomValue } from "@effect/atom-react";
import { createFileRoute } from "@tanstack/react-router";
import { Option } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";

/**
 * The rider's surface: book a trip, then watch it happen.
 *
 * Nothing on this page polls. The trip service pushes every change to the
 * people on the trip, and `tripPushesAtom` — mounted for as long as the page is
 * open — turns each push into an invalidation, so the status below changes the
 * moment the server records it.
 */
const Ride = () => {
  useAtomMount(tripPushesAtom);
  // The hold, the bank's check and the receipt move by payment pushes, not
  // trip ones.
  useAtomMount(paymentPushesAtom);
  const role = useAtomValue(
    sessionAtom,
    (session) => AsyncResult.isSuccess(session) ? session.value.role : undefined,
  );
  const active = useAtomValue(activeTripAtom);

  // The trip list is scoped by whoever holds the token, so a driver here would
  // see the trips they drove presented as rides they booked.
  if (role === "driver") {
    return (
      <p className="text-muted-foreground text-sm">
        This page is for riders. Your account drives — the Drive page is where offers arrive.
      </p>
    );
  }

  if (AsyncResult.isInitial(active)) {
    return <p className="text-muted-foreground text-sm">Loading your trips…</p>;
  }
  if (AsyncResult.isFailure(active)) return <QueryError result={active} subject="your trips" />;

  return Option.isSome(active.value) ? <RiderTrip /> : <BookRide />;
};

export const Route = createFileRoute("/_protected/ride/")({
  // Client-only: the page holds a WebSocket and a WebGL map, and neither has
  // anything to render on the server.
  ssr: false,
  staticData: { crumb: "Ride" },
  component: Ride,
});
