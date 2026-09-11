import { sessionAtom } from "@/atom/session-atoms.js";
import { QueryError } from "@/components/app/query-error.js";
import { BookRide } from "@/routes/_protected/ride/-components/book-ride.js";
import { RiderTrip } from "@/routes/_protected/ride/-components/rider-trip.js";
import { useAtomMount, useAtomValue } from "@effect/atom-react";
import { paymentPushesAtom } from "@surge/common/atom/payment-atoms";
import { activeTripAtom, tripPushesAtom } from "@surge/common/atom/trip-atoms";
import { createFileRoute, Link } from "@tanstack/react-router";
import { Option } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";
import type * as React from "react";

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
      <Notice>
        This page is for riders. Your account drives, and offers arrive on the{" "}
        <Link to="/drive" className="font-semibold text-foreground underline">Drive</Link> page.
      </Notice>
    );
  }

  if (AsyncResult.isInitial(active)) return <Notice>Loading your trips…</Notice>;
  if (AsyncResult.isFailure(active)) {
    return (
      <Notice>
        <QueryError result={active} subject="your trips" />
      </Notice>
    );
  }

  return Option.isSome(active.value) ? <RiderTrip /> : <BookRide />;
};

/** The page is full-bleed, so the few states without a map bring their own margin. */
const Notice = (props: { readonly children: React.ReactNode; }) => (
  <div className="flex-1 p-4 text-[15px] text-muted-foreground md:p-8">{props.children}</div>
);

export const Route = createFileRoute("/_protected/ride/")({
  // Client-only: the page holds a WebSocket and a WebGL map, and neither has
  // anything to render on the server.
  ssr: false,
  staticData: { crumb: "Ride", fullBleed: true },
  component: Ride,
});
