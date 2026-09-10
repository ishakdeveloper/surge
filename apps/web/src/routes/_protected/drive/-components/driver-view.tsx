import { shiftAtom } from "@/atom/driver-atoms.js";
import { answeredAtom, nowAtom, offersAtom } from "@/atom/realtime-atoms.js";
import { activeTripAtom } from "@/atom/trip-atoms.js";
import { SplitView } from "@/components/app/split-view.js";
import { type MapMarker, SurgeMap } from "@/components/map/surge-map.js";
import { DriverTrip } from "@/routes/_protected/drive/-components/driver-trip.js";
import { OfferList } from "@/routes/_protected/drive/-components/offer-list.js";
import { ShiftPanel } from "@/routes/_protected/drive/-components/shift-panel.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { decodePolyline6 } from "@surge/domain/geo/Polyline";
import { Option, Result } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";

/**
 * Everything a driver sees at once: their shift, then either the trip they are
 * on or the offers waiting for them, and the map under all of it.
 *
 * Clicking the map places the driver. On a laptop that is the honest way to be
 * somewhere — a browser's own position is an IP guess good to a few hundred
 * metres — and it is how a demo stands a driver on a rider's pickup.
 */
export const DriverView = () => {
  const shift = useAtomValue(shiftAtom);
  const setShift = useAtomSet(shiftAtom);
  const trip = useAtomValue(activeTripAtom).pipe(AsyncResult.getOrThrow);
  const offers = useAtomValue(offersAtom);
  const answered = useAtomValue(answeredAtom);
  const now = useAtomValue(
    nowAtom,
    (result) => Option.getOrElse(AsyncResult.value(result), () => Date.now()),
  );

  const waiting = AsyncResult.isSuccess(offers)
    ? offers.value.filter((offer) => offer.expiresAtMs > now && !answered.tripIds.has(offer.tripId))
    : [];

  const self: ReadonlyArray<MapMarker> = Option.toArray(
    Option.map(shift.position, (position): MapMarker => ({ id: "self", position, kind: "self" })),
  );

  const map = Option.match(trip, {
    onNone: () => ({
      markers: [
        ...self,
        ...waiting.map((offer): MapMarker => ({
          id: offer.tripId,
          position: { lat: offer.pickupLat, lng: offer.pickupLng },
          kind: "pickup",
        })),
      ],
      route: [],
      follow: Option.toArray(shift.position),
    }),
    onSome: (current) => ({
      markers: [
        ...self,
        { id: "pickup", position: current.pickup, kind: "pickup" } satisfies MapMarker,
        { id: "dropoff", position: current.dropoff, kind: "dropoff" } satisfies MapMarker,
      ],
      route: Result.getOrElse(decodePolyline6(current.route.polyline6), () => []),
      follow: [current.pickup, current.dropoff],
    }),
  });

  return (
    <SplitView
      aside={
        <>
          <header className="flex flex-col gap-1">
            <h1 className="text-lg font-semibold">Drive</h1>
            <p className="text-muted-foreground text-sm">
              Click the map to place yourself, then go online.
            </p>
          </header>
          <ShiftPanel />
          {Option.isSome(trip)
            ? <DriverTrip />
            : <OfferList offers={waiting} now={now} online={shift.online} />}
        </>
      }
    >
      <SurgeMap
        markers={map.markers}
        route={map.route}
        follow={map.follow}
        onPick={(position) => {
          setShift({ ...shift, position: Option.some(position) });
        }}
      />
    </SplitView>
  );
};
