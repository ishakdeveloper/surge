import { bookTrip, previewTrip, tripsAtom } from "@/atom/trip-atoms.js";
import { ActionError, errorCode } from "@/components/app/action-error.js";
import { SplitView } from "@/components/app/split-view.js";
import { type MapMarker, type MapPoint, SurgeMap } from "@/components/map/surge-map.js";
import { Alert, AlertDescription } from "@/components/ui/alert.js";
import { Button } from "@/components/ui/button.js";
import {
  formatCents,
  formatDistance,
  formatDuration,
  formatPoint,
  riderStatus,
} from "@/lib/format.js";
import { cn } from "@/lib/utils.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import type { FareId } from "@surge/domain/api/Primitives";
import { decodePolyline6 } from "@surge/domain/geo/Polyline";
import { Option, Result } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";
import * as React from "react";

/**
 * Trips worth one click. Real Amsterdam addresses on roads, so a demo does not
 * begin with finding somewhere a car can actually stop.
 */
const PRESETS: ReadonlyArray<
  { readonly label: string; readonly pickup: MapPoint; readonly dropoff: MapPoint; }
> = [
  {
    label: "Centraal → Rijksmuseum",
    pickup: { lat: 52.3791, lng: 4.9003 },
    dropoff: { lat: 52.36, lng: 4.8852 },
  },
  {
    label: "Sloterdijk → Westerpark",
    pickup: { lat: 52.3889, lng: 4.8377 },
    dropoff: { lat: 52.3868, lng: 4.8752 },
  },
  {
    label: "Zuid → De Pijp",
    pickup: { lat: 52.3389, lng: 4.8723 },
    dropoff: { lat: 52.3533, lng: 4.8946 },
  },
];

interface Choice {
  readonly fareId: FareId;
  /**
   * One key per chosen fare, kept while it stays chosen. Pressing Book again
   * after a failure then retries the same booking, and the trip service
   * returns the trip it already made instead of a second one.
   */
  readonly idempotencyKey: string;
}

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

  React.useEffect(() => {
    if (Option.isSome(pickup) && Option.isSome(dropoff)) {
      requestQuote({ pickup: pickup.value, dropoff: dropoff.value });
    }
  }, [pickup, dropoff, requestQuote]);

  const place = (point: MapPoint) => {
    setChoice(Option.none());
    // Pickup first, then dropoff; a third click starts over from the pickup.
    if (Option.isNone(pickup) || Option.isSome(dropoff)) {
      setPickup(Option.some(point));
      setDropoff(Option.none());
    } else {
      setDropoff(Option.some(point));
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
    <SplitView
      aside={
        <>
          <header className="flex flex-col gap-1">
            <h1 className="text-lg font-semibold">Ride</h1>
            <p className="text-muted-foreground text-sm">
              Click the map for a pickup, then a dropoff.
            </p>
          </header>

          {latest !== undefined
            && (latest.status === "TRIP_STATUS_UNMATCHED"
              || latest.status === "TRIP_STATUS_CANCELLED")
            && (
              <Alert>
                <AlertDescription>
                  Your last trip: {riderStatus[latest.status].toLowerCase()}{" "}
                  <span className="text-muted-foreground font-mono text-xs">{latest.status}</span>
                </AlertDescription>
              </Alert>
            )}

          <section className="flex flex-col gap-3" aria-labelledby="where">
            <h2 id="where" className="text-sm font-medium">Where</h2>
            <dl className="grid grid-cols-[4.5rem_1fr] gap-1.5 text-sm">
              <dt className="text-muted-foreground">Pickup</dt>
              <dd className="font-mono text-xs">
                {Option.match(pickup, { onNone: () => "-", onSome: formatPoint })}
              </dd>
              <dt className="text-muted-foreground">Dropoff</dt>
              <dd className="font-mono text-xs">
                {Option.match(dropoff, { onNone: () => "-", onSome: formatPoint })}
              </dd>
            </dl>
            <div className="flex flex-wrap gap-2">
              {PRESETS.map((preset) => (
                <Button
                  key={preset.label}
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    setChoice(Option.none());
                    setPickup(Option.some(preset.pickup));
                    setDropoff(Option.some(preset.dropoff));
                  }}
                >
                  {preset.label}
                </Button>
              ))}
            </div>
          </section>

          {Option.isSome(pickup) && Option.isSome(dropoff) && (
            <section
              className="flex flex-col gap-3"
              aria-labelledby="fares"
              aria-busy={quote.waiting}
            >
              <h2 id="fares" className="text-sm font-medium">Fares</h2>

              {AsyncResult.isFailure(quote) && <ActionError cause={quote.cause} />}
              {quote.waiting && !AsyncResult.isSuccess(quote) && (
                <p className="text-muted-foreground text-sm">Pricing the route…</p>
              )}

              {AsyncResult.isSuccess(quote) && (
                <>
                  <p className="text-muted-foreground text-sm">
                    {formatDistance(quote.value.route.meters)} · about{" "}
                    {formatDuration(quote.value.route.seconds)}
                  </p>
                  <ul className="flex flex-col gap-2">
                    {quote.value.fares.map((fare) => {
                      const chosen = Option.isSome(choice) && choice.value.fareId === fare.fareId;
                      return (
                        <li key={fare.fareId}>
                          <button
                            type="button"
                            aria-pressed={chosen}
                            onClick={() => {
                              setChoice((current) =>
                                Option.isSome(current) && current.value.fareId === fare.fareId
                                  ? current
                                  : Option.some({
                                    fareId: fare.fareId,
                                    // A click handler needs the key now, not an Effect to run
                                    // for it; it only has to be unique, which this is.
                                    // oxlint-disable-next-line effecttsgo/crypto-random-uuid
                                    idempotencyKey: crypto.randomUUID(),
                                  })
                              );
                            }}
                            className={cn(
                              "flex w-full items-center justify-between rounded-md border px-3 py-2 text-left text-sm",
                              chosen ? "border-primary" : "border-border",
                            )}
                          >
                            <span className="font-mono text-xs">{fare.packageSlug}</span>
                            <span className="flex items-baseline gap-2">
                              {fare.surgeMultiplier > 1 && (
                                <span className="text-muted-foreground font-mono text-xs">
                                  ×{fare.surgeMultiplier.toFixed(2)}
                                </span>
                              )}
                              <span className="tabular-nums">{formatCents(fare.totalCents)}</span>
                            </span>
                          </button>
                        </li>
                      );
                    })}
                  </ul>

                  {AsyncResult.isFailure(booking) && <ActionError cause={booking.cause} />}
                  {expired && (
                    <Button
                      type="button"
                      variant="outline"
                      onClick={() => {
                        setChoice(Option.none());
                        requestQuote({ pickup: pickup.value, dropoff: dropoff.value });
                      }}
                    >
                      Get a new quote
                    </Button>
                  )}

                  <Button
                    type="button"
                    disabled={Option.isNone(choice) || booking.waiting}
                    onClick={() => {
                      if (Option.isSome(choice)) book(choice.value);
                    }}
                  >
                    {booking.waiting ? "Booking…" : "Book this ride"}
                  </Button>
                </>
              )}
            </section>
          )}
        </>
      }
    >
      <SurgeMap
        markers={markers}
        route={route}
        follow={route.length > 1
          ? [route[0]!, route[route.length - 1]!]
          : markers.map((marker) => marker.position)}
        onPick={place}
      />
    </SplitView>
  );
};
