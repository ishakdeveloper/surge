import { DriverId } from "../api/Primitives.js";
import { TripsGet200, type TripsPreview200 } from "../api/SurgeApi.js";

/**
 * Names for the shapes the generated client returns, and the rules about them
 * that no document can state.
 *
 * `../api/SurgeApi.ts` is generated from `proto/trip.proto` and inlines every
 * nested message into the response that carries it, which is right for a
 * generator and wrong for the rest of a codebase — `Trip` should be a word you
 * can write in a function signature. These are that word, derived rather than
 * restated, so a field that moves in the proto moves here on the next
 * `make proto` without anything to keep in step.
 */

/** A trip, as the API returns it. */
export type Trip = TripsGet200["trip"];

/** A quote for one vehicle class. */
export type FareQuote = TripsPreview200["fares"][number];

export type Route = Trip["route"];

/**
 * A WGS84 coordinate. `lng`, not `lon` — one spelling across H3, Valhalla, the
 * Go services and here is worth more than any argument about which.
 */
export type Coordinate = Trip["pickup"];

/**
 * Where a trip is.
 *
 * The protobuf enum names, verbatim. A friendlier spelling here would be one
 * more place for the two sides to disagree; the place to make it readable is
 * where it is rendered.
 *
 * A runtime schema, lifted out of the generated client rather than restated,
 * because the WebSocket's trip push decodes the same field and should fail on
 * the same values the REST response would.
 */
export const TripStatus = TripsGet200.fields.trip.fields.status;
export type TripStatus = typeof TripStatus.Type;

/** Statuses a rider is still waiting through: for the fare to be held, then for a driver. */
export const isPending = (status: TripStatus): boolean =>
  status === "TRIP_STATUS_PAYMENT_PENDING"
  || status === "TRIP_STATUS_REQUESTED"
  || status === "TRIP_STATUS_OFFERED";

/** Statuses where a driver is on their way or the ride is under way. */
export const isUnderway = (status: TripStatus): boolean =>
  status === "TRIP_STATUS_ACCEPTED"
  || status === "TRIP_STATUS_ARRIVED"
  || status === "TRIP_STATUS_IN_PROGRESS";

/** Statuses nothing further will happen from. */
export const isFinished = (status: TripStatus): boolean =>
  status === "TRIP_STATUS_COMPLETED" || status === "TRIP_STATUS_CANCELLED";

/**
 * Whether a driver has been assigned.
 *
 * The gateway marshals with `EmitUnpopulated`, so an unassigned trip carries
 * `""` rather than omitting the field — which is the useful behaviour, and the
 * reason this exists rather than an `undefined` check that would never fire.
 */
export const assignedDriver = (trip: Trip): DriverId | undefined =>
  trip.driverId === "" ? undefined : DriverId.make(trip.driverId);
