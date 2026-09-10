import type { TripStatus } from "@surge/domain/trip/Trip";

const euros = new Intl.NumberFormat("nl-NL", { style: "currency", currency: "EUR" });

/** Money is minor units on the wire and everywhere else; this is the one place it becomes euros. */
export const formatCents = (cents: number): string => euros.format(cents / 100);

export const formatDistance = (meters: number): string =>
  meters < 1000 ? `${Math.round(meters)} m` : `${(meters / 1000).toFixed(1)} km`;

export const formatDuration = (seconds: number): string =>
  `${Math.max(1, Math.round(seconds / 60))} min`;

export const formatPoint = (point: { readonly lat: number; readonly lng: number; }): string =>
  `${point.lat.toFixed(5)}, ${point.lng.toFixed(5)}`;

/**
 * What a rider is told. The raw enum is shown beside it wherever this is used —
 * `dashboard-ui.md` keeps technical identifiers exact — and this is the sentence
 * for a person.
 */
export const riderStatus: Record<TripStatus, string> = {
  TRIP_STATUS_UNSPECIFIED: "Unknown",
  TRIP_STATUS_REQUESTED: "Finding you a driver",
  TRIP_STATUS_OFFERED: "Finding you a driver",
  TRIP_STATUS_ACCEPTED: "Your driver is on the way",
  TRIP_STATUS_ARRIVED: "Your driver is at the pickup",
  TRIP_STATUS_IN_PROGRESS: "On your way",
  TRIP_STATUS_COMPLETED: "You have arrived",
  TRIP_STATUS_CANCELLED: "Cancelled",
  TRIP_STATUS_UNMATCHED: "No driver accepted",
};

export const driverStatus: Record<TripStatus, string> = {
  ...riderStatus,
  TRIP_STATUS_ACCEPTED: "Drive to the pickup",
  TRIP_STATUS_ARRIVED: "Waiting for the rider",
  TRIP_STATUS_IN_PROGRESS: "Drive to the dropoff",
  TRIP_STATUS_COMPLETED: "Trip complete",
};
