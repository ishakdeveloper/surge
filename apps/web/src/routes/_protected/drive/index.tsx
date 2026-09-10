import { pingLoopAtom } from "@/atom/driver-atoms.js";
import { sessionAtom } from "@/atom/session-atoms.js";
import { activeTripAtom, tripPushesAtom } from "@/atom/trip-atoms.js";
import { QueryError } from "@/components/app/query-error.js";
import { DriverView } from "@/routes/_protected/drive/-components/driver-view.js";
import { useAtomMount, useAtomValue } from "@effect/atom-react";
import { createFileRoute } from "@tanstack/react-router";
import { AsyncResult } from "effect/unstable/reactivity";

/**
 * The driver's surface: be somewhere, go online, take a ride, finish it.
 *
 * Two things run for as long as this page is open. The ping loop reports the
 * driver's position every four seconds — with a status that follows the trip,
 * not the toggle, once there is one — and the push bridge turns trip updates
 * into refreshed queries, which is how an accepted offer becomes the trip on
 * screen without the page asking.
 */
const Drive = () => {
  useAtomMount(tripPushesAtom);
  useAtomMount(pingLoopAtom);
  const role = useAtomValue(
    sessionAtom,
    (session) => AsyncResult.isSuccess(session) ? session.value.role : undefined,
  );
  const active = useAtomValue(activeTripAtom);

  // The gateway refuses a rider's pings and the trip service a rider's
  // arrivals regardless; this only says so before anybody tries.
  if (role !== undefined && role !== "driver") {
    return (
      <p className="text-muted-foreground text-sm">
        This page is for drivers. Your account rides — the Ride page is where trips are booked.
      </p>
    );
  }

  if (AsyncResult.isInitial(active)) {
    return <p className="text-muted-foreground text-sm">Loading your trips…</p>;
  }
  if (AsyncResult.isFailure(active)) return <QueryError result={active} subject="your trips" />;

  return <DriverView />;
};

export const Route = createFileRoute("/_protected/drive/")({
  // Client-only for the same reason as the ride page: a socket and a WebGL map.
  ssr: false,
  staticData: { crumb: "Drive" },
  component: Drive,
});
