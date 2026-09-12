import { ActionError } from "@/components/app/action-error.js";
import { AfterTripChat } from "@/components/chat/counterpart-card.js";
import { type MapMarker, type MapPoint, SurgeMap } from "@/components/map/surge-map.js";
import { useCity } from "@/components/map/use-city.js";
import {
  actionVariants,
  IconBubble,
  pressable,
  Sign,
  signVariants,
} from "@/components/sign/sign.js";
import { cn } from "@/lib/utils.js";
import { CardSummary } from "@/routes/_protected/ride/-components/card-summary.js";
import {
  PlaceField,
  type Stop,
  StopsCard,
  SwapStops,
} from "@/routes/_protected/ride/-components/place-field.js";
import { RideLayout, useRideMapInset } from "@/routes/_protected/ride/-components/ride-layout.js";
import { TripReceipt } from "@/routes/_protected/ride/-components/trip-payment.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import { cardAtom } from "@surge/common/atom/payment-atoms";
import { nearbyPickupAtom, type PlaceField as Field } from "@surge/common/atom/place-atoms";
import { bookTrip, previewTrip, tripsAtom } from "@surge/common/atom/trip-atoms";
import { errorCode } from "@surge/common/lib/cause";
import { formatCents, formatDistance, formatDuration, riderStatus } from "@surge/common/lib/format";
import { rideClass } from "@surge/common/ride/classes";
import { RIDE_PRESETS } from "@surge/common/ride/presets";
import { type FareId, UserId } from "@surge/domain/api/Primitives";
import { decodePolyline6 } from "@surge/domain/geo/Polyline";
import { isFinished } from "@surge/domain/trip/Trip";
import { Link } from "@tanstack/react-router";
import { Array as Arr, Option, Result } from "effect";
import { AsyncResult } from "effect/unstable/reactivity";
import { ArrowRight, Check, CreditCard, LoaderCircle } from "lucide-react";
import * as React from "react";

interface Choice {
  readonly fareId: FareId;
  /**
   * One key per chosen fare, kept while it stays chosen. Pressing Go again
   * after a failure then retries the same booking, and the trip service
   * returns the trip it already made instead of a second one.
   */
  readonly idempotencyKey: string;
}

// A click handler needs the key now, not an Effect to run for it; it only has
// to be unique, which this is.
// oxlint-disable-next-line effecttsgo/crypto-random-uuid
const newKey = () => crypto.randomUUID();

const at = (position: MapPoint): Stop => ({ position, place: Option.none() });

/**
 * The rider's page before a trip, as one sheet: where from and where to, the
 * classes priced for that route, the card the fare is held on, and Go. While a
 * stop is being searched for, everything under it steps aside.
 */
export const BookRide = () => {
  const [pickup, setPickup] = React.useState<Option.Option<Stop>>(Option.none());
  const [dropoff, setDropoff] = React.useState<Option.Option<Stop>>(Option.none());
  const [choice, setChoice] = React.useState<Option.Option<Choice>>(Option.none());
  const [editing, setEditing] = React.useState<Option.Option<Field>>(Option.none());
  const inset = useRideMapInset();
  const city = useCity();

  const quote = useAtomValue(previewTrip);
  const requestQuote = useAtomSet(previewTrip);
  const booking = useAtomValue(bookTrip);
  const book = useAtomSet(bookTrip);
  const nearby = useAtomValue(nearbyPickupAtom);
  const latest = useAtomValue(
    tripsAtom,
    (trips) => AsyncResult.isSuccess(trips) ? trips.value[0] : undefined,
  );
  // Only a known answer of "no card" holds Go back. A card query that failed
  // says nothing about the card, and the trip service is the one that decides
  // whether a booking needs one.
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
      : { fareId: fare.fareId, idempotencyKey: newKey() };
    setChoice(Option.some(current));
    book(current);
  };

  const goLabel = Option.isNone(pickup) || Option.isNone(dropoff)
    ? "Choose where to"
    : quote.waiting
    ? "Pricing the route…"
    : Option.isNone(chosen)
    ? "No price for this route"
    : booking.waiting
    ? "Booking…"
    : `Go · ${rideClass(chosen.value.packageSlug).label}`;
  const ready = Option.isSome(chosen) && !noCard && !booking.waiting;

  const searching = Option.getOrUndefined(editing);

  /** The price and the arrow in its black well, as Go and "Add a card" carry them. */
  const priceAndArrow = Option.isSome(chosen) && (
    <span className="flex items-center gap-3 tabular-nums">
      {formatCents(chosen.value.totalCents)}
      <span
        aria-hidden
        className="grid size-10 place-items-center rounded-full bg-primary-foreground text-primary transition-[transform,background-color,color] duration-200 ease-out group-hover:translate-x-0.5 group-disabled:translate-x-0 group-disabled:bg-foreground/10 group-disabled:text-muted-foreground"
      >
        <ArrowRight className="size-5" />
      </span>
    </span>
  );
  const goClass =
    "group flex h-14 w-full cursor-pointer items-center justify-between gap-3 rounded-full bg-primary py-2 pr-2 pl-6 text-left text-[17px] font-bold text-primary-foreground transition-[background-color,color,transform] duration-150 ease-out hover:bg-[#f5c900] active:scale-[0.985] disabled:cursor-not-allowed disabled:bg-tile disabled:text-muted-foreground disabled:active:scale-100";

  // The next step, pinned under the thumb. Without a saved card the next step
  // is to add one, so Go becomes the way there rather than a grey dead end.
  const footer = searching !== undefined ? null : (
    <div className="flex flex-col gap-2">
      {noCard && Option.isSome(chosen)
        ? (
          <Link to="/ride/payment" className={goClass}>
            <span>Add a card to book</span>
            {priceAndArrow}
          </Link>
        )
        : (
          <button type="button" disabled={!ready} onClick={go} className={goClass}>
            <span className="flex items-center gap-2">
              {booking.waiting && <LoaderCircle className="size-5 motion-safe:animate-spin" />}
              {goLabel}
            </span>
            {priceAndArrow}
          </button>
        )}
      {Option.isSome(chosen) && (
        <p className="px-4 text-center text-xs leading-snug text-muted-foreground">
          {noCard
            ? "Your fare will be held on it"
            : `Booking holds ${formatCents(chosen.value.totalCents)} on your card`}. You are charged
          when the trip ends, and a cancelled trip releases the hold.
        </p>
      )}
    </div>
  );
  const field = (name: Field) => ({
    editing: searching === name,
    onEdit: () => {
      setEditing(Option.some(name));
    },
    onDone: () => {
      setEditing(Option.none());
    },
  });

  return (
    <RideLayout
      title="Book a ride"
      footer={footer}
      map={
        <SurgeMap
          markers={markers}
          route={line}
          // The whole route, not its ends: a trip around the centre bows well
          // outside the box its pickup and dropoff make.
          follow={line.length > 1 ? line : markers.map((marker) => marker.position)}
          inset={inset}
          onPick={pick}
          // The free cars around, and where others are booking: the city the
          // rider is about to ride in, live, under their own route.
          cars={city.cars}
          flashes={city.flashes}
          onView={city.onView}
        />
      }
    >
      <StopsCard
        rail={searching === undefined}
        footer={Option.isSome(priced) && searching === undefined
          ? (
            <p className="px-3 pt-0.5 pb-1.5 text-right text-sm font-semibold tabular-nums motion-safe:animate-rise-in">
              {formatDuration(priced.value.route.seconds)} ·{" "}
              {formatDistance(priced.value.route.meters)}
            </p>
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
            <section
              aria-labelledby="popular-trips"
              className="flex flex-col gap-2 px-1 pt-1 pb-1 motion-safe:animate-rise-in"
            >
              <h2 id="popular-trips" className="text-[13px] font-semibold text-muted-foreground">
                Popular trips
              </h2>
              <div className="flex flex-wrap gap-2">
                {RIDE_PRESETS.map((preset) => (
                  <button
                    key={preset.label}
                    type="button"
                    onClick={() => {
                      route(Option.some(at(preset.pickup)), Option.some(at(preset.dropoff)));
                    }}
                    className="cursor-pointer rounded-full bg-tile px-3.5 py-2 text-sm font-semibold transition-[background-color,transform] duration-150 ease-out hover:bg-tile-hover active:scale-95"
                  >
                    {preset.label}
                  </button>
                ))}
              </div>
            </section>
          )}

          {Option.isSome(pickup) && Option.isSome(dropoff) && (
            <>
              {quote.waiting && (
                <Sign tone="choice" className="items-center" aria-busy>
                  <IconBubble>
                    <LoaderCircle className="motion-safe:animate-spin" />
                  </IconBubble>
                  <p className="text-[15px] font-semibold">Pricing the route…</p>
                </Sign>
              )}
              {AsyncResult.isFailure(quote) && !quote.waiting && (
                <ActionError cause={quote.cause} />
              )}

              {Option.isSome(priced) && (
                <div
                  role="radiogroup"
                  aria-label="Class"
                  className="flex flex-col gap-2 motion-safe:animate-rise-in"
                >
                  {priced.value.fares.map((fare) => {
                    const selected = Option.isSome(chosen) && chosen.value.fareId === fare.fareId;
                    const named = rideClass(fare.packageSlug);
                    return (
                      <button
                        key={fare.fareId}
                        type="button"
                        role="radio"
                        aria-checked={selected}
                        onClick={() => {
                          setChoice((current) =>
                            Option.isSome(current) && current.value.fareId === fare.fareId
                              ? current
                              : Option.some({ fareId: fare.fareId, idempotencyKey: newKey() })
                          );
                        }}
                        className={cn(
                          signVariants({ tone: selected ? "direction" : "choice" }),
                          pressable,
                          "w-full items-center text-left",
                          !selected && "hover:bg-tile-hover",
                        )}
                      >
                        {
                          /* The radio's own mark: an empty ring, filled with a
                            check that pops in when this class is chosen. */
                        }
                        <span
                          aria-hidden
                          className={cn(
                            "grid size-6 shrink-0 place-items-center rounded-full transition-colors duration-150",
                            selected
                              ? "bg-foreground text-primary"
                              : "ring-2 ring-foreground/20 ring-inset",
                          )}
                        >
                          {selected && (
                            <Check
                              className="size-3.5 motion-safe:animate-pop-in"
                              strokeWidth={3}
                            />
                          )}
                        </span>
                        <span className="flex min-w-0 flex-1 flex-col">
                          <span className="text-base font-semibold">{named.label}</span>
                          <span className="text-[13px] opacity-70">
                            {Option.match(named.seats, {
                              onNone: () => fare.packageSlug,
                              onSome: (seats) => `${seats} seats`,
                            })}
                            {fare.surgeMultiplier > 1
                              && ` · busy, ×${fare.surgeMultiplier.toFixed(2)}`}
                          </span>
                        </span>
                        <span className="text-[22px] leading-none font-bold tabular-nums">
                          {formatCents(fare.totalCents)}
                        </span>
                      </button>
                    );
                  })}
                </div>
              )}
            </>
          )}

          <Sign tone="service" className="items-center">
            <IconBubble className="bg-white/10">
              <CreditCard />
            </IconBubble>
            <div className="min-w-0 flex-1">
              <CardSummary />
            </div>
            {/* Without a card, Go is the way to add one; this only changes it. */}
            {!noCard && (
              <Link to="/ride/payment" className={actionVariants({ size: "sm" })}>
                Change
              </Link>
            )}
          </Sign>

          {AsyncResult.isFailure(booking) && <ActionError cause={booking.cause} />}
          {expired && Option.isSome(pickup) && Option.isSome(dropoff) && (
            <button
              type="button"
              onClick={() => {
                route(pickup, dropoff);
              }}
              className={cn(
                signVariants({ tone: "choice" }),
                pressable,
                "w-full justify-center text-[15px] font-semibold hover:bg-tile-hover",
              )}
            >
              That price has expired. Get a new quote
            </button>
          )}

          {
            /* The driver of the trip just finished, still in reach for the hour
              its conversation stays open. */
          }
          {latest !== undefined && latest.status === "TRIP_STATUS_COMPLETED"
            && latest.driverId !== "" && (
            <AfterTripChat
              tripId={latest.id}
              userId={UserId.make(latest.driverId)}
              role="Your driver"
            />
          )}

          {latest !== undefined
            && (isFinished(latest.status) || latest.status === "TRIP_STATUS_UNMATCHED")
            && (
              <Sign tone="choice" className="mt-2 flex-col gap-3">
                <h2 className="text-base font-semibold">
                  Last trip: {riderStatus[latest.status].toLowerCase()}
                </h2>
                <TripReceipt tripId={latest.id} />
              </Sign>
            )}
        </>
      )}
    </RideLayout>
  );
};
