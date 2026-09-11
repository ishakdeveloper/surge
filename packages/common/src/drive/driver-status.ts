import type { TripId } from "@surge/domain/api/Primitives";
import type { DriverStatus, Offer } from "@surge/domain/realtime/Wire";
import type { TripStatus } from "@surge/domain/trip/Trip";
import { Array, Option } from "effect";

/**
 * What the matcher is told, derived — the driver never chooses it.
 *
 * The trip decides first. A driver on their way to a pickup who kept reporting
 * `idle` would be offered a second ride while delivering the first, so once a
 * trip is accepted the status follows the trip, whatever the toggle says. The
 * same rule as `apps/web/src/atom/driver-atoms.ts`, and it must stay the same:
 * the matcher cannot tell which app a ping came from.
 */
export const statusFor = (online: boolean, trip: Option.Option<TripStatus>): DriverStatus => {
  if (Option.isSome(trip)) {
    if (trip.value === "TRIP_STATUS_ACCEPTED") return "enroute_pickup";
    if (trip.value === "TRIP_STATUS_ARRIVED" || trip.value === "TRIP_STATUS_IN_PROGRESS") {
      return "on_trip";
    }
  }
  return online ? "idle" : "offline";
};

/**
 * Offers still worth showing: not yet expired, and not already answered.
 *
 * One derivation, used by the list and by its count in the tab badge, so the
 * two cannot disagree about what is on offer.
 */
export const openOffers = (
  offers: ReadonlyArray<Offer>,
  answered: ReadonlySet<TripId>,
  now: number,
): ReadonlyArray<Offer> =>
  Array.filter(offers, (offer) => offer.expiresAtMs > now && !answered.has(offer.tripId));

/** Whole seconds an offer has left, never negative. */
export const secondsLeft = (offer: Offer, now: number): number =>
  Math.max(0, Math.ceil((offer.expiresAtMs - now) / 1000));
