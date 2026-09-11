import { Announced } from "@/components/app/announced.js";
import { ActionError } from "@/components/app/errors.js";
import { type MapMarker, type MapPoint, SurgeMap } from "@/components/map/surge-map.js";
import { Action, IconBubble, Sign, SignText, type Tone } from "@/components/sign/sign.js";
import { Text } from "@/components/ui/text.js";
import { colors } from "@/lib/theme.js";
import { type Stop, StopName, StopRow, StopsCard } from "@/ride/place-field.js";
import { RideLayout } from "@/ride/ride-layout.js";
import { HoldStep } from "@/ride/trip-payment.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { driverPositionAtom } from "@surge/common/atom/realtime-atoms";
import { activeTripAtom, cancelTrip } from "@surge/common/atom/trip-atoms";
import { formatCents, formatDistance, formatDuration, riderStatus } from "@surge/common/lib/format";
import { decodePolyline6 } from "@surge/domain/geo/Polyline";
import { isPending, isUnderway, type Trip } from "@surge/domain/trip/Trip";
import { Option, Result } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";
import * as React from "react";
import { ActivityIndicator, View } from "react-native";
import Animated, {
  Easing,
  ReduceMotion,
  useAnimatedStyle,
  useSharedValue,
  withTiming,
} from "react-native-reanimated";

/**
 * A trip in flight, from the rider's side. Reads the active trip itself, per
 * `knowledge/rules/effect-atom.md`: the screen has guarded loading and
 * failure, and a push moving the trip re-renders this on its own.
 */
export const RiderTrip = () => {
  const active = useAtomValue(activeTripAtom).pipe(AsyncResult.getOrThrow);
  const cancelling = useAtomValue(cancelTrip);
  const cancel = useAtomSet(cancelTrip);

  // The screen swaps this out when the trip finishes; this covers the render
  // in between rather than throwing.
  if (Option.isNone(active)) return null;
  const trip = active.value;
  const route = Result.getOrElse(decodePolyline6(trip.route.polyline6), () => []);

  // The state machine allows cancelling until the rider is in the car.
  const cancellable = isPending(trip.status) || trip.status === "TRIP_STATUS_ACCEPTED"
    || trip.status === "TRIP_STATUS_ARRIVED";
  const pickup: Stop = { position: trip.pickup, place: Option.none() };
  const dropoff: Stop = { position: trip.dropoff, place: Option.none() };

  return (
    <RideLayout
      // Cancelling is the one action a waiting rider has, so it sits where Go
      // sat: under the thumb, until the rider is in the car.
      footer={cancellable
        ? (
          <Action
            label={cancelling.waiting ? "Cancelling…" : "Cancel trip"}
            onPress={() => {
              cancel({ tripId: trip.id, reason: "rider cancelled" });
            }}
            tone="danger"
            size="block"
            disabled={cancelling.waiting}
          />
        )
        : null}
      map={isUnderway(trip.status) && trip.driverId !== ""
        ? <FollowedMap trip={trip} route={route} />
        : (
          <SurgeMap
            markers={stops(trip)}
            route={route}
            follow={route.length > 1 ? route : [trip.pickup, trip.dropoff]}
          />
        )}
    >
      {/* Announced as it changes: this is the update a rider is waiting for. */}
      <Announced message={riderStatus[trip.status]} className="">
        <Wipe key={trip.status}>
          <StatusCard trip={trip} pickup={pickup} dropoff={dropoff} />
        </Wipe>
      </Announced>

      {trip.status === "TRIP_STATUS_PAYMENT_PENDING" && (
        <HoldStep tripId={trip.id} totalCents={trip.totalCents} />
      )}

      <StopsCard
        rail
        footer={
          <Text className="px-3 pt-0.5 pb-1.5 text-right text-sm font-semibold tabular-nums">
            {formatDuration(trip.route.seconds)} · {formatDistance(trip.route.meters)}
          </Text>
        }
      >
        <StopRow kind="pickup" label="From" stop={pickup} />
        <StopRow kind="dropoff" label="To" stop={dropoff} />
      </StopsCard>

      <Sign>
        <IconBubble name="card-outline" />
        <SignText className="flex-1 text-base font-semibold">Fare</SignText>
        <SignText className="text-[22px] font-bold tabular-nums">
          {formatCents(trip.totalCents)}
        </SignText>
      </Sign>

      {AsyncResult.isFailure(cancelling) && <ActionError cause={cancelling.cause} />}

      <Text className="px-1 pt-1 text-center text-xs text-muted-foreground">
        Trip <Text className="font-mono text-xs text-muted-foreground">{trip.id}</Text>
      </Text>
    </RideLayout>
  );
};

/**
 * The status card's entrance, as on the web: a reveal from the left edge
 * rather than a fade, so a new status reads as the next sign sliding into the
 * frame. The width animates over the card's measured width; Reanimated skips
 * it when the system asks for reduced motion.
 */
const Wipe = (props: { readonly children: React.ReactNode; }) => {
  const [full, setFull] = React.useState(0);
  const width = useSharedValue(0);
  const revealing = useAnimatedStyle(() => ({ width: width.value }));

  return (
    <View
      onLayout={(event) => {
        const measured = event.nativeEvent.layout.width;
        if (full !== 0 || measured === 0) return;
        setFull(measured);
        width.value = withTiming(measured, {
          duration: 280,
          easing: Easing.out(Easing.exp),
          reduceMotion: ReduceMotion.System,
        });
      }}
    >
      <Animated.View style={[{ overflow: "hidden", borderRadius: 16 }, revealing]}>
        <View style={{ width: full === 0 ? undefined : full }}>{props.children}</View>
      </Animated.View>
    </View>
  );
};

/**
 * The card at the top of the sheet: what is happening now, in the colour that
 * says what kind of news it is.
 */
const StatusCard = (props: {
  readonly trip: Trip;
  readonly pickup: Stop;
  readonly dropoff: Stop;
}) => {
  const { status } = props.trip;
  const tone: Tone = status === "TRIP_STATUS_PAYMENT_PENDING"
    ? "service"
    : status === "TRIP_STATUS_ARRIVED"
    ? "done"
    : "direction";

  return (
    <Sign tone={tone} className="py-4">
      {status === "TRIP_STATUS_REQUESTED" || status === "TRIP_STATUS_OFFERED"
        ? (
          <View className="size-10 items-center justify-center rounded-full bg-card">
            <ActivityIndicator color={colors.foreground} />
          </View>
        )
        : (
          <IconBubble
            name={status === "TRIP_STATUS_PAYMENT_PENDING"
              ? "card-outline"
              : status === "TRIP_STATUS_ARRIVED"
              ? "checkmark"
              : "car-outline"}
          />
        )}
      <View className="min-w-0 flex-1 gap-0.5">
        <SignText className="text-xl leading-tight font-bold">{riderStatus[status]}</SignText>
        <SignText className="text-sm opacity-80">
          {status === "TRIP_STATUS_PAYMENT_PENDING"
            ? "The fare is held before a driver is asked."
            : status === "TRIP_STATUS_REQUESTED" || status === "TRIP_STATUS_OFFERED"
            ? "Offering your trip to drivers nearby."
            : status === "TRIP_STATUS_ACCEPTED"
            ? <DriverEta tripId={props.trip.id} />
            : status === "TRIP_STATUS_ARRIVED"
            ? (
              <>
                Meet them at <StopName stop={props.pickup} />.
              </>
            )
            : status === "TRIP_STATUS_IN_PROGRESS"
            ? (
              <>
                To <StopName stop={props.dropoff} />.
              </>
            )
            : null}
        </SignText>
      </View>
    </Sign>
  );
};

/**
 * The predicted wait while the driver is on the way. Its own component because
 * following is a subscription: mounting asks the gateway for the driver's
 * position, and unmounting stops it.
 */
const DriverEta = (props: { readonly tripId: Trip["id"]; }) => {
  const position = useAtomValue(driverPositionAtom(props.tripId));
  return AsyncResult.isSuccess(position) && position.value.etaSeconds > 0
    ? <>Arriving in about {formatDuration(position.value.etaSeconds)}.</>
    : <>Heading to your pickup.</>;
};

const stops = (trip: Trip): ReadonlyArray<MapMarker> => [
  { id: "pickup", position: trip.pickup, kind: "pickup" },
  { id: "dropoff", position: trip.dropoff, kind: "dropoff" },
];

/**
 * The map once a driver is assigned, with the driver on it — the web's
 * `FollowedMap`. The camera stays on the trip rather than chasing the car, so
 * a rider watching it approach is not also watching the map lurch.
 */
const FollowedMap = (props: { readonly trip: Trip; readonly route: ReadonlyArray<MapPoint>; }) => {
  const position = useAtomValue(driverPositionAtom(props.trip.id));
  const driver: ReadonlyArray<MapMarker> = AsyncResult.isSuccess(position)
    ? [{
      id: "driver",
      position: { lat: position.value.lat, lng: position.value.lng },
      kind: "driver",
    }]
    : [];

  return (
    <SurgeMap
      markers={[...stops(props.trip), ...driver]}
      route={props.route}
      follow={props.route.length > 1 ? props.route : [props.trip.pickup, props.trip.dropoff]}
    />
  );
};
