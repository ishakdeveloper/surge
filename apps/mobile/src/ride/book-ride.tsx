import { ActionError } from "@/components/app/errors.js";
import { type MapMarker, type MapPoint, SurgeMap } from "@/components/map/surge-map.js";
import { IconBubble, Sign, SignButton, SignText } from "@/components/sign/sign.js";
import { Text } from "@/components/ui/text.js";
import { colors } from "@/lib/theme.js";
import { cn } from "@/lib/utils.js";
import { CardAction, CardActionError, CardSummary, useCardAction } from "@/ride/card-summary.js";
import { PlaceField, type Stop, StopsCard, SwapStops } from "@/ride/place-field.js";
import { RideLayout } from "@/ride/ride-layout.js";
import { TripReceipt } from "@/ride/trip-payment.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import Ionicons from "@expo/vector-icons/Ionicons";
import { cardAtom } from "@surge/common/atom/payment-atoms";
import { nearbyPickupAtom, type PlaceField as Field } from "@surge/common/atom/place-atoms";
import { bookTrip, previewTrip, tripsAtom } from "@surge/common/atom/trip-atoms";
import { errorCode } from "@surge/common/lib/cause";
import { formatCents, formatDistance, formatDuration, riderStatus } from "@surge/common/lib/format";
import { rideClass } from "@surge/common/ride/classes";
import { RIDE_PRESETS } from "@surge/common/ride/presets";
import type { FareId } from "@surge/domain/api/Primitives";
import { decodePolyline6 } from "@surge/domain/geo/Polyline";
import { isFinished } from "@surge/domain/trip/Trip";
import { Array as Arr, Option, Result } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";
import { randomUUID } from "expo-crypto";
import * as React from "react";
import { ActivityIndicator, Pressable, View } from "react-native";
import Animated, { FadeInDown, ZoomIn } from "react-native-reanimated";

interface Choice {
  readonly fareId: FareId;
  /**
   * One key per chosen fare, kept while it stays chosen. Pressing Go again
   * after a failure then retries the same booking, and the trip service
   * returns the trip it already made instead of a second one.
   */
  readonly idempotencyKey: string;
}

const at = (position: MapPoint): Stop => ({ position, place: Option.none() });

/** New content rising into the sheet: a short, soft entrance. */
const rise = FadeInDown.duration(220);

/**
 * The rider's screen before a trip, as one sheet: where from and where to, the
 * classes priced for that route, the card the fare is held on, and Go pinned
 * under the thumb. While a stop is being searched for, everything under it
 * steps aside.
 */
export const BookRide = () => {
  const [pickup, setPickup] = React.useState<Option.Option<Stop>>(Option.none());
  const [dropoff, setDropoff] = React.useState<Option.Option<Stop>>(Option.none());
  const [choice, setChoice] = React.useState<Option.Option<Choice>>(Option.none());
  const [editing, setEditing] = React.useState<Option.Option<Field>>(Option.none());

  const quote = useAtomValue(previewTrip);
  const requestQuote = useAtomSet(previewTrip);
  const booking = useAtomValue(bookTrip);
  const book = useAtomSet(bookTrip);
  const nearby = useAtomValue(nearbyPickupAtom);
  const cardAction = useCardAction();
  const latest = useAtomValue(
    tripsAtom,
    (trips) => AsyncResult.isSuccess(trips) ? trips.value[0] : undefined,
  );
  // Only a known answer of "no card" holds Go back. A card query that failed
  // says nothing about the card, and the trip service decides whether a
  // booking needs one.
  const noCard = useAtomValue(
    cardAtom,
    (card) => AsyncResult.isSuccess(card) && !card.value.saved,
  );

  /** Both stops, priced the moment both exist. A new stop forgets the class chosen. */
  const route = (from: Option.Option<Stop>, to: Option.Option<Stop>) => {
    setChoice(Option.none());
    setPickup(from);
    setDropoff(to);
    if (Option.isSome(from) && Option.isSome(to)) {
      requestQuote({ pickup: from.value.position, dropoff: to.value.position });
    }
  };

  // The pickup starts where the rider is, once, when that is in Amsterdam. A
  // refused or failed position just leaves the field for them to fill.
  const prefilled = React.useRef(false);
  React.useEffect(() => {
    if (prefilled.current || !AsyncResult.isSuccess(nearby)) return;
    prefilled.current = true;
    if (Option.isSome(nearby.value) && Option.isNone(pickup)) {
      route(Option.some(at(nearby.value.value)), dropoff);
    }
    // Runs on the position arriving; the stops are read, not watched.
    // oxlint-disable-next-line react-hooks/exhaustive-deps
  }, [nearby]);

  // The map picks the pickup first, then the dropoff; a third tap starts over.
  const pick = (point: MapPoint) => {
    if (Option.isSome(pickup) && Option.isNone(dropoff)) {
      route(pickup, Option.some(at(point)));
    } else {
      route(Option.some(at(point)), Option.none());
    }
  };

  // A quote being replaced is not a quote to book from: its fares belong to
  // the old stops, and booking one would book the old route.
  const priced = AsyncResult.isSuccess(quote) && !quote.waiting
      && Option.isSome(pickup) && Option.isSome(dropoff)
    ? Option.some(quote.value)
    : Option.none();

  const chosen = Option.flatMap(priced, (current) =>
    Option.match(choice, {
      onNone: () => Arr.head(current.fares),
      onSome: (picked) => Arr.findFirst(current.fares, (fare) => fare.fareId === picked.fareId),
    }));

  const line = Option.match(priced, {
    onNone: () => [],
    onSome: (current) => Result.getOrElse(decodePolyline6(current.route.polyline6), () => []),
  });

  const markers: ReadonlyArray<MapMarker> = [
    ...Option.toArray(Option.map(pickup, (stop): MapMarker => ({
      id: "pickup",
      position: stop.position,
      kind: "pickup",
    }))),
    ...Option.toArray(Option.map(dropoff, (stop): MapMarker => ({
      id: "dropoff",
      position: stop.position,
      kind: "dropoff",
    }))),
  ];

  const expired = AsyncResult.isFailure(booking)
    && Option.getOrUndefined(errorCode(booking.cause)) === "failed_precondition";

  const go = () => {
    if (Option.isNone(chosen)) return;
    const fare = chosen.value;
    const current = Option.isSome(choice) && choice.value.fareId === fare.fareId
      ? choice.value
      : { fareId: fare.fareId, idempotencyKey: randomUUID() };
    setChoice(Option.some(current));
    book(current);
  };

  // Without a saved card the next step is to add one, so Go becomes that
  // rather than a grey dead end.
  const addingCard = noCard && Option.isSome(chosen);
  const goLabel = Option.isNone(pickup) || Option.isNone(dropoff)
    ? "Choose where to"
    : quote.waiting
    ? "Pricing the route…"
    : Option.isNone(chosen)
    ? "No price for this route"
    : addingCard
    ? cardAction.waiting ? cardAction.label : "Add a card to book"
    : booking.waiting
    ? "Booking…"
    : `Go · ${rideClass(chosen.value.packageSlug).label}`;
  const ready = Option.isSome(chosen)
    && (addingCard ? !cardAction.waiting : !noCard && !booking.waiting);

  const searching = Option.getOrUndefined(editing);
  const field = (name: Field) => ({
    editing: searching === name,
    onEdit: () => {
      setEditing(Option.some(name));
    },
    onDone: () => {
      setEditing(Option.none());
    },
  });

  const footer = searching !== undefined ? null : (
    <View className="gap-2">
      <Pressable
        accessibilityRole="button"
        accessibilityState={{ disabled: !ready, busy: booking.waiting || cardAction.waiting }}
        disabled={!ready}
        onPress={addingCard ? cardAction.run : go}
        className="h-14 flex-row items-center justify-between gap-3 rounded-full bg-primary py-2 pr-2 pl-6 active:scale-[0.985] active:bg-[#f0c400] disabled:bg-tile"
      >
        <View className="flex-row items-center gap-2">
          {(booking.waiting || cardAction.waiting) && (
            <ActivityIndicator color={colors.mutedForeground} />
          )}
          <Text
            className={cn(
              "text-[17px] font-bold",
              ready ? "text-primary-foreground" : "text-muted-foreground",
            )}
          >
            {goLabel}
          </Text>
        </View>
        {Option.isSome(chosen) && (
          <View className="flex-row items-center gap-3">
            <Text
              className={cn(
                "text-[17px] font-bold tabular-nums",
                ready ? "text-primary-foreground" : "text-muted-foreground",
              )}
            >
              {formatCents(chosen.value.totalCents)}
            </Text>
            <View
              className={cn(
                "size-10 items-center justify-center rounded-full",
                ready ? "bg-foreground" : "bg-foreground/10",
              )}
            >
              <Ionicons
                name="arrow-forward"
                size={20}
                color={ready ? colors.primary : colors.mutedForeground}
              />
            </View>
          </View>
        )}
      </Pressable>
      {Option.isSome(chosen) && (
        <Text className="px-4 text-center text-xs leading-snug text-muted-foreground">
          {noCard
            ? "Your fare will be held on it."
            : `Booking holds ${formatCents(chosen.value.totalCents)} on your card.`}{" "}
          You are charged when the trip ends, and a cancelled trip releases the hold.
        </Text>
      )}
    </View>
  );

  return (
    <RideLayout
      footer={footer}
      map={
        <SurgeMap
          markers={markers}
          route={line}
          // The whole route, not its ends: a trip around the centre bows well
          // outside the box its pickup and dropoff make.
          follow={line.length > 1 ? line : markers.map((marker) => marker.position)}
          onPick={pick}
        />
      }
    >
      <StopsCard
        rail={searching === undefined}
        footer={Option.isSome(priced) && searching === undefined
          ? (
            <Animated.View entering={rise}>
              <Text className="px-3 pt-0.5 pb-1.5 text-right text-sm font-semibold tabular-nums">
                {formatDuration(priced.value.route.seconds)} ·{" "}
                {formatDistance(priced.value.route.meters)}
              </Text>
            </Animated.View>
          )
          : null}
      >
        <PlaceField
          field="pickup"
          label="From"
          placeholder="Where are you?"
          stop={pickup}
          {...field("pickup")}
          onChoose={(stop) => {
            route(Option.some(stop), dropoff);
          }}
        />
        <PlaceField
          field="dropoff"
          label="To"
          placeholder="Where to?"
          stop={dropoff}
          {...field("dropoff")}
          onChoose={(stop) => {
            route(pickup, Option.some(stop));
          }}
        />
        {searching === undefined && Option.isSome(pickup) && Option.isSome(dropoff) && (
          <SwapStops
            onSwap={() => {
              route(dropoff, pickup);
            }}
          />
        )}
      </StopsCard>

      {searching === undefined && (
        <>
          {Option.isNone(dropoff) && (
            <Animated.View entering={rise} className="gap-2 px-1 pt-1 pb-1">
              <Text
                accessibilityRole="header"
                className="text-[13px] font-semibold text-muted-foreground"
              >
                Popular trips
              </Text>
              <View className="flex-row flex-wrap gap-2">
                {RIDE_PRESETS.map((preset) => (
                  <Pressable
                    key={preset.label}
                    accessibilityRole="button"
                    onPress={() => {
                      route(Option.some(at(preset.pickup)), Option.some(at(preset.dropoff)));
                    }}
                    className="rounded-full bg-tile px-3.5 py-2 active:scale-95 active:bg-tile-hover"
                  >
                    <Text className="text-sm font-semibold">{preset.label}</Text>
                  </Pressable>
                ))}
              </View>
            </Animated.View>
          )}

          {Option.isSome(pickup) && Option.isSome(dropoff) && (
            <>
              {quote.waiting && (
                <Sign accessibilityState={{ busy: true }}>
                  <View className="size-10 items-center justify-center rounded-full bg-card">
                    <ActivityIndicator color={colors.foreground} />
                  </View>
                  <SignText className="font-semibold">Pricing the route…</SignText>
                </Sign>
              )}
              {AsyncResult.isFailure(quote) && !quote.waiting && (
                <ActionError cause={quote.cause} />
              )}

              {Option.isSome(priced) && (
                <Animated.View
                  entering={rise}
                  accessibilityRole="radiogroup"
                  accessibilityLabel="Class"
                  className="gap-2"
                >
                  {priced.value.fares.map((fare) => {
                    const selected = Option.isSome(chosen) && chosen.value.fareId === fare.fareId;
                    const named = rideClass(fare.packageSlug);
                    return (
                      <SignButton
                        key={fare.fareId}
                        tone={selected ? "direction" : "choice"}
                        accessibilityRole="radio"
                        accessibilityState={{ checked: selected }}
                        onPress={() => {
                          setChoice((current) =>
                            Option.isSome(current) && current.value.fareId === fare.fareId
                              ? current
                              : Option.some({ fareId: fare.fareId, idempotencyKey: randomUUID() })
                          );
                        }}
                      >
                        {
                          /* The radio's own mark: an empty ring, filled with a
                            check that pops in when this class is chosen. */
                        }
                        <View
                          className={cn(
                            "size-6 items-center justify-center rounded-full",
                            selected ? "bg-foreground" : "border-2 border-foreground/20",
                          )}
                        >
                          {selected && (
                            <Animated.View entering={ZoomIn.duration(180)}>
                              <Ionicons name="checkmark" size={15} color={colors.primary} />
                            </Animated.View>
                          )}
                        </View>
                        <View className="min-w-0 flex-1">
                          <SignText className="text-base font-semibold">{named.label}</SignText>
                          <SignText className="text-[13px] opacity-70">
                            {Option.match(named.seats, {
                              onNone: () => fare.packageSlug,
                              onSome: (seats) => `${seats} seats`,
                            })}
                            {fare.surgeMultiplier > 1
                              && ` · busy, ×${fare.surgeMultiplier.toFixed(2)}`}
                          </SignText>
                        </View>
                        <SignText className="text-[22px] font-bold tabular-nums">
                          {formatCents(fare.totalCents)}
                        </SignText>
                      </SignButton>
                    );
                  })}
                </Animated.View>
              )}
            </>
          )}

          <Sign tone="service">
            <IconBubble name="card-outline" />
            <View className="min-w-0 flex-1">
              <CardSummary />
            </View>
            {/* Without a card, Go is the way to add one; this only changes it. */}
            {!noCard && <CardAction />}
          </Sign>
          <CardActionError />

          {AsyncResult.isFailure(booking) && <ActionError cause={booking.cause} />}
          {expired && Option.isSome(pickup) && Option.isSome(dropoff) && (
            <SignButton
              className="justify-center"
              onPress={() => {
                route(pickup, dropoff);
              }}
            >
              <SignText className="font-semibold">That price has expired. Get a new quote</SignText>
            </SignButton>
          )}

          {latest !== undefined
            && (isFinished(latest.status) || latest.status === "TRIP_STATUS_UNMATCHED")
            && (
              <Sign className="mt-2 flex-col items-stretch gap-3">
                <SignText accessibilityRole="header" className="text-base font-semibold">
                  Last trip: {riderStatus[latest.status].toLowerCase()}
                </SignText>
                <TripReceipt tripId={latest.id} />
              </Sign>
            )}
        </>
      )}
    </RideLayout>
  );
};
