import { ActionError } from "@/components/app/errors.js";
import { Screen } from "@/components/app/screen.js";
import { type MapMarker, type MapPoint, SurgeMap } from "@/components/map/surge-map.js";
import { Alert, AlertTitle } from "@/components/ui/alert.js";
import { Button } from "@/components/ui/button.js";
import { Card } from "@/components/ui/card.js";
import { Heading, Text } from "@/components/ui/text.js";
import { cn } from "@/lib/utils.js";
import { CardSummary } from "@/ride/card-summary.js";
import { TripReceipt } from "@/ride/trip-payment.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { cardAtom } from "@surge/common/atom/payment-atoms";
import { bookTrip, previewTrip, tripsAtom } from "@surge/common/atom/trip-atoms";
import { errorCode } from "@surge/common/lib/cause";
import {
  formatCents,
  formatDistance,
  formatDuration,
  formatPoint,
  riderStatus,
} from "@surge/common/lib/format";
import { RIDE_PRESETS } from "@surge/common/ride/presets";
import type { FareId } from "@surge/domain/api/Primitives";
import { decodePolyline6 } from "@surge/domain/geo/Polyline";
import { isFinished, type Trip } from "@surge/domain/trip/Trip";
import { Option, Result } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";
import { randomUUID } from "expo-crypto";
import * as React from "react";
import { Pressable, View } from "react-native";

interface Choice {
  readonly fareId: FareId;
  /**
   * One key per chosen fare, kept while it stays chosen. Pressing Book again
   * after a failure then retries the same booking, and the trip service
   * returns the trip it already made instead of a second one.
   */
  readonly idempotencyKey: string;
}

const samePoint = (a: MapPoint, b: MapPoint) => a.lat === b.lat && a.lng === b.lng;

/** The rider's surface before a trip: where, how much, book. */
export const BookRide = () => {
  const [pickup, setPickup] = React.useState<Option.Option<MapPoint>>(Option.none());
  const [dropoff, setDropoff] = React.useState<Option.Option<MapPoint>>(Option.none());
  const [choice, setChoice] = React.useState<Option.Option<Choice>>(Option.none());

  const quote = useAtomValue(previewTrip);
  const requestQuote = useAtomSet(previewTrip);
  const booking = useAtomValue(bookTrip);
  const book = useAtomSet(bookTrip);
  const latest = useAtomValue(
    tripsAtom,
    (trips) => AsyncResult.isSuccess(trips) ? trips.value[0] : undefined,
  );
  // Only a known answer of "no card" holds the button back. A card query that
  // failed says nothing about the card, and the trip service decides whether a
  // booking needs one.
  const noCard = useAtomValue(
    cardAtom,
    (card) => AsyncResult.isSuccess(card) && !card.value.saved,
  );

  /** A pickup, and a dropoff once there is one — priced the moment both exist. */
  const choose = (from: MapPoint, to: Option.Option<MapPoint>) => {
    setChoice(Option.none());
    setPickup(Option.some(from));
    setDropoff(to);
    if (Option.isSome(to)) requestQuote({ pickup: from, dropoff: to.value });
  };

  // Pickup first, then dropoff; a third tap starts over from the pickup.
  const place = (point: MapPoint) => {
    if (Option.isSome(pickup) && Option.isNone(dropoff)) {
      choose(pickup.value, Option.some(point));
    } else {
      choose(point, Option.none());
    }
  };

  const route = AsyncResult.isSuccess(quote)
    ? Result.getOrElse(decodePolyline6(quote.value.route.polyline6), () => [])
    : [];

  const markers: ReadonlyArray<MapMarker> = [
    ...Option.toArray(
      Option.map(pickup, (position): MapMarker => ({ id: "pickup", position, kind: "pickup" })),
    ),
    ...Option.toArray(
      Option.map(dropoff, (position): MapMarker => ({ id: "dropoff", position, kind: "dropoff" })),
    ),
  ];

  const expired = AsyncResult.isFailure(booking)
    && Option.getOrUndefined(errorCode(booking.cause)) === "failed_precondition";

  return (
    <Screen>
      {latest !== undefined
        && (isFinished(latest.status) || latest.status === "TRIP_STATUS_UNMATCHED")
        && <LastTrip trip={latest} />}

      <SurgeMap
        markers={markers}
        route={route}
        follow={route.length > 1
          ? [route[0]!, route[route.length - 1]!]
          : markers.map((marker) => marker.position)}
        onPick={place}
      />

      <Card>
        <Heading>Payment</Heading>
        <CardSummary />
      </Card>

      <View className="gap-3">
        <Heading>Where</Heading>
        <Text className="text-muted-foreground">
          Tap the map for a pickup, then a dropoff — or choose a trip.
        </Text>
        {RIDE_PRESETS.map((option) => {
          const selected = Option.isSome(pickup) && Option.isSome(dropoff)
            && samePoint(pickup.value, option.pickup) && samePoint(dropoff.value, option.dropoff);
          return (
            <Pressable
              key={option.label}
              accessibilityRole="radio"
              accessibilityState={{ checked: selected }}
              onPress={() => {
                choose(option.pickup, Option.some(option.dropoff));
              }}
              className={cn(
                "gap-1 rounded-lg border px-3 py-3",
                selected ? "border-primary" : "border-border",
              )}
            >
              <Text className="font-medium">{option.label}</Text>
              <Text className="font-mono text-xs text-muted-foreground">
                {formatPoint(option.pickup)} → {formatPoint(option.dropoff)}
              </Text>
            </Pressable>
          );
        })}
      </View>

      {Option.isSome(pickup) && Option.isSome(dropoff) && (
        <View className="gap-3" accessibilityState={{ busy: quote.waiting }}>
          <Heading>Fares</Heading>

          {AsyncResult.isFailure(quote) && <ActionError cause={quote.cause} />}
          {quote.waiting && !AsyncResult.isSuccess(quote) && (
            <Text className="text-muted-foreground">Pricing the route…</Text>
          )}

          {AsyncResult.isSuccess(quote) && (
            <>
              <Text className="text-muted-foreground">
                {formatDistance(quote.value.route.meters)} · about{" "}
                {formatDuration(quote.value.route.seconds)}
              </Text>
              {quote.value.fares.map((fare) => {
                const chosen = Option.isSome(choice) && choice.value.fareId === fare.fareId;
                return (
                  <Pressable
                    key={fare.fareId}
                    accessibilityRole="radio"
                    accessibilityState={{ checked: chosen }}
                    onPress={() => {
                      setChoice((current) =>
                        Option.isSome(current) && current.value.fareId === fare.fareId
                          ? current
                          : Option.some({ fareId: fare.fareId, idempotencyKey: randomUUID() })
                      );
                    }}
                    className={cn(
                      "flex-row items-center justify-between rounded-lg border px-3 py-3",
                      chosen ? "border-primary" : "border-border",
                    )}
                  >
                    <Text className="font-mono text-xs">{fare.packageSlug}</Text>
                    <View className="flex-row items-baseline gap-2">
                      {fare.surgeMultiplier > 1 && (
                        <Text className="font-mono text-xs text-muted-foreground">
                          ×{fare.surgeMultiplier.toFixed(2)}
                        </Text>
                      )}
                      <Text className="tabular-nums">{formatCents(fare.totalCents)}</Text>
                    </View>
                  </Pressable>
                );
              })}

              {AsyncResult.isFailure(booking) && <ActionError cause={booking.cause} />}
              {expired && (
                <Button
                  variant="outline"
                  onPress={() => {
                    choose(pickup.value, dropoff);
                  }}
                >
                  Get a new quote
                </Button>
              )}

              <Button
                disabled={Option.isNone(choice) || booking.waiting || noCard}
                onPress={() => {
                  if (Option.isSome(choice)) book(choice.value);
                }}
              >
                {booking.waiting ? "Booking…" : "Book this ride"}
              </Button>
            </>
          )}
        </View>
      )}
    </Screen>
  );
};

const LastTrip = (props: { readonly trip: Trip; }) => (
  <Alert>
    <AlertTitle>
      Your last trip: {riderStatus[props.trip.status].toLowerCase()}
    </AlertTitle>
    <Text className="font-mono text-xs text-muted-foreground">{props.trip.status}</Text>
    <TripReceipt tripId={props.trip.id} />
  </Alert>
);
