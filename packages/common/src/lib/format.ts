import type {
  Earning,
  PaymentStatus,
  PayoutAccount,
  Withdrawal,
} from "@surge/domain/payments/Payment";
import type { TripStatus } from "@surge/domain/trip/Trip";
import { DateTime } from "effect";

const euros = new Intl.NumberFormat("nl-NL", { style: "currency", currency: "EUR" });

/** Money is minor units on the wire and everywhere else; this is the one place it becomes euros. */
export const formatCents = (cents: number): string => euros.format(cents / 100);

const moment = new Intl.DateTimeFormat("nl-NL", { dateStyle: "medium", timeStyle: "short" });

/** An RFC 3339 timestamp from the API, in the reader's time zone. */
export const formatTime = (rfc3339: string): string =>
  moment.format(DateTime.toDate(DateTime.makeUnsafe(rfc3339)));

/** A card as a rider recognises it. The brand stays Stripe's own word. */
export const formatExpiry = (month: number, year: number): string =>
  `${String(month).padStart(2, "0")}/${year}`;

/** What became of a rider's hold, in a sentence. The raw enum is shown beside it. */
export const paymentStatus: Record<PaymentStatus, string> = {
  PAYMENT_STATUS_UNSPECIFIED: "Unknown",
  PAYMENT_STATUS_AUTHORIZING: "Placing a hold on your card",
  PAYMENT_STATUS_REQUIRES_ACTION: "Waiting for your bank to confirm",
  PAYMENT_STATUS_AUTHORIZED: "Held on your card",
  PAYMENT_STATUS_CAPTURED: "Charged",
  PAYMENT_STATUS_RELEASED: "Hold released, nothing charged",
  PAYMENT_STATUS_FAILED: "Payment failed",
  PAYMENT_STATUS_REFUNDED: "Refunded",
};

/**
 * Why a payment failed, for the rider. Keyed by the service's reason codes;
 * one it adds later falls through to the code itself rather than to nothing.
 */
const paymentFailures: Readonly<Record<string, string>> = {
  no_payment_method: "No card was saved. Add one, then book again.",
  declined: "Your card was declined. Try another card.",
  authentication_failed: "Your bank did not confirm the payment.",
  expired: "The hold expired before the trip finished.",
};

export const paymentFailure = (reason: string): string => paymentFailures[reason] ?? reason;

export const payoutStatus: Record<PayoutAccount["status"], string> = {
  PAYOUT_ACCOUNT_STATUS_UNSPECIFIED: "Unknown",
  PAYOUT_ACCOUNT_STATUS_NOT_STARTED: "Not set up",
  PAYOUT_ACCOUNT_STATUS_PENDING: "Being verified",
  PAYOUT_ACCOUNT_STATUS_ACTIVE: "Active",
};

export const earningStatus: Record<Earning["status"], string> = {
  EARNING_STATUS_UNSPECIFIED: "Unknown",
  EARNING_STATUS_UNPAID: "Owed",
  EARNING_STATUS_TRANSFERRED: "In balance",
  EARNING_STATUS_REVERSED: "Taken back",
};

export const withdrawalStatus: Record<Withdrawal["status"], string> = {
  WITHDRAWAL_STATUS_UNSPECIFIED: "Unknown",
  WITHDRAWAL_STATUS_REQUESTED: "Requested",
  WITHDRAWAL_STATUS_IN_TRANSIT: "On its way",
  WITHDRAWAL_STATUS_PAID: "Paid",
  WITHDRAWAL_STATUS_FAILED: "Failed",
};

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
  TRIP_STATUS_PAYMENT_PENDING: "Confirming your payment",
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
