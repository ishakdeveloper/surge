import { QueryError } from "@/components/app/errors.js";
import { Screen } from "@/components/app/screen.js";
import { type MapMarker, SurgeMap } from "@/components/map/surge-map.js";
import { Text } from "@/components/ui/text.js";
import { DriverTrip, DriverTripStep } from "@/drive/driver-trip.js";
import { OfferPopup } from "@/drive/offer-list.js";
import { ShiftPanel } from "@/drive/shift-panel.js";
import { RideLayout } from "@/ride/ride-layout.js";
import { useAtomMount, useAtomSet, useAtomValue } from "@effect/atom-react";
import {
  followAtom,
  followingAtom,
  pingLoopAtom,
  shiftAtom,
} from "@surge/common/atom/driver-atoms";
import { answeredAtom, nowAtom, offersAtom } from "@surge/common/atom/realtime-atoms";
import { activeTripAtom, tripPushesAtom } from "@surge/common/atom/trip-atoms";
import { openOffers } from "@surge/common/drive/driver-status";
import { decodePolyline6 } from "@surge/domain/geo/Polyline";
import { Option, Result } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";
import { View } from "react-native";

/**
 * The driver's surface: be somewhere, go online, take a ride, finish it.
 *
 * Three things run while this tab is mounted — and a tab stays mounted once
 * visited, so switching to Earnings does not drop the driver off the map: the
 * ping loop, the device's own fixes when the driver follows GPS, and the push
 * bridge that turns an accepted offer into the trip on screen without asking.
 */
const Drive = () => {
  useAtomMount(tripPushesAtom);
  useAtomMount(pingLoopAtom);
  useAtomMount(followAtom);
  const active = useAtomValue(activeTripAtom);

  if (AsyncResult.isInitial(active)) {
    return (
      <Screen title="Drive">
        <Text className="text-muted-foreground">Loading your trips…</Text>
      </Screen>
    );
  }
  if (AsyncResult.isFailure(active)) {
    return (
      <Screen title="Drive">
        <QueryError result={active} subject="your trips" />
      </Screen>
    );
  }

  return <DriverView />;
};

/**
 * Everything a driver sees at once, laid out as the rider's screen is: the map
 * across the top, and a sheet rising over it with the shift — or, on a trip,
 * the trip and its next step pinned under the thumb. An offer rises over the
 * sheet as its own card, the way a ride app puts a request in front of a
 * driver, and goes when it is answered or runs out.
 *
 * The open offers are worked out once, here, for both the card and the map's
 * pickup markers — two derivations of one list would be two answers to "what
 * is on offer". Tapping the map places the driver there and stops following
 * GPS, which is how a demo stands a driver on a rider's pickup.
 */
const DriverView = () => {
  const shift = useAtomValue(shiftAtom);
  const setShift = useAtomSet(shiftAtom);
  const setFollowing = useAtomSet(followingAtom);
  const trip = useAtomValue(activeTripAtom).pipe(AsyncResult.getOrThrow);
  const offers = useAtomValue(offersAtom);
  const answered = useAtomValue(answeredAtom);
  const now = useAtomValue(
    nowAtom,
    (result) => Option.getOrElse(AsyncResult.value(result), () => Date.now()),
  );

  // No offers yet is the stream not having emitted, not an empty list being
  // withheld: `offersAtom` only yields once the first offer arrives.
  const waiting = AsyncResult.isSuccess(offers)
    ? openOffers(offers.value, answered.tripIds, now)
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
    <View className="flex-1">
      <RideLayout
        map={
          <SurgeMap
            markers={map.markers}
            route={map.route}
            follow={map.follow}
            onPick={(position) => {
              setFollowing(false);
              setShift({ ...shift, position: Option.some(position) });
            }}
          />
        }
        footer={Option.isSome(trip) ? <DriverTripStep /> : null}
      >
        {Option.isSome(trip) ? <DriverTrip /> : <ShiftPanel />}
      </RideLayout>
      {Option.isNone(trip) && (
        <OfferPopup offers={waiting} now={now} position={shift.position} online={shift.online} />
      )}
    </View>
  );
};

export default Drive;
