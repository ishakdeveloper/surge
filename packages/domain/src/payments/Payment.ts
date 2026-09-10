import {
  type PaymentsGetBalance200,
  PaymentsGetForTrip200,
  type PaymentsGetMethod200,
  type PaymentsGetPayoutAccount200,
  type PaymentsListEarnings200,
  type PaymentsListWithdrawals200,
} from "../api/SurgeApi.js";

/**
 * Names for the payment shapes the generated client returns, derived rather
 * than restated — the same arrangement as `../trip/Trip.ts`, for the same
 * reason: a field that moves in `proto/payments.proto` moves here on the next
 * `make proto`, with nothing to keep in step.
 */

/** What became of a trip's hold. */
export type Payment = PaymentsGetForTrip200["payment"];

/** The card a rider's holds are placed on. */
export type Card = PaymentsGetMethod200["card"];

/** Whether a driver can be paid yet. */
export type PayoutAccount = PaymentsGetPayoutAccount200["account"];

/** A driver's share of one trip. */
export type Earning = PaymentsListEarnings200["earnings"][number];

/** What a driver has, and whether they can take it out. */
export type Balance = PaymentsGetBalance200["balance"];

/** One payout to a driver's bank. */
export type Withdrawal = PaymentsListWithdrawals200["withdrawals"][number];

/**
 * Where a payment is. The protobuf enum names, verbatim, as a runtime schema
 * lifted out of the generated client — the same choice `TripStatus` makes.
 */
export const PaymentStatus = PaymentsGetForTrip200.fields.payment.fields.status;
export type PaymentStatus = typeof PaymentStatus.Type;

/** The rider has an authentication step to finish before the trip can go. */
export const awaitsRider = (payment: Payment): boolean =>
  payment.status === "PAYMENT_STATUS_REQUIRES_ACTION" && payment.clientSecret !== "";

/** Money is held, or being asked for, on the rider's card. */
export const isHolding = (status: PaymentStatus): boolean =>
  status === "PAYMENT_STATUS_AUTHORIZING"
  || status === "PAYMENT_STATUS_REQUIRES_ACTION"
  || status === "PAYMENT_STATUS_AUTHORIZED";
