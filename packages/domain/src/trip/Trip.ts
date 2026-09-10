import { Schema, SchemaGetter } from "effect";

/**
 * Branded ids. Construct with `TripId.make(value)`, which validates — never
 * cast with `as`.
 */
export const TripId = Schema.String.pipe(Schema.brand("TripId")).annotate({
  identifier: "TripId",
});
export type TripId = typeof TripId.Type;

export const FareId = Schema.String.pipe(Schema.brand("FareId")).annotate({
  identifier: "FareId",
});
export type FareId = typeof FareId.Type;

export const DriverId = Schema.String.pipe(Schema.brand("DriverId")).annotate({
  identifier: "DriverId",
});
export type DriverId = typeof DriverId.Type;

/**
 * A WGS84 coordinate. `lng`, not `lon` — one spelling across H3, Valhalla, the
 * Go services and here is worth more than any argument about which.
 */
export class Coordinate extends Schema.Class<Coordinate>("Coordinate")({
  lat: Schema.Number.check(Schema.isBetween({ minimum: -90, maximum: 90 })),
  lng: Schema.Number.check(Schema.isBetween({ minimum: -180, maximum: 180 })),
}) {}

/**
 * proto3 JSON encodes int64 and uint64 as *strings*, because a JSON number
 * loses precision past 2^53. Every 64-bit field on the wire therefore arrives
 * quoted, and a schema expecting a number rejects it — which is exactly what
 * happened, and is the kind of thing only a test against the real server finds.
 *
 * `double` and `float` are not affected: those stay JSON numbers.
 */
export const Int64FromString = Schema.String.pipe(
  Schema.decodeTo(Schema.Number, {
    decode: SchemaGetter.transform((value) => Number(value)),
    encode: SchemaGetter.transform((value) => String(value)),
  }),
).annotate({ identifier: "Int64FromString" });

/**
 * A driveable path.
 *
 * `polyline6` rather than an array of coordinates: a city route is hundreds of
 * points, and encoded it is roughly a tenth the bytes on a connection a phone
 * is paying for. Precision 6, as Valhalla emits it — decoding it at 5 yields a
 * well-formed route that is wrong by a factor of ten.
 */
export class Route extends Schema.Class<Route>("Route")({
  polyline6: Schema.String,
  // A double on the wire, so a plain number.
  meters: Schema.Number,
  // An int64 on the wire, so a string.
  seconds: Int64FromString,
}) {}

/**
 * Where a trip is.
 *
 * The wire carries the protobuf enum names, so the union is spelled the way the
 * Go services spell it rather than being prettified here — a translation table
 * in the client is one more place for the two to disagree.
 */
export const TripStatus = Schema.Literals([
  "TRIP_STATUS_UNSPECIFIED",
  "TRIP_STATUS_REQUESTED",
  "TRIP_STATUS_OFFERED",
  "TRIP_STATUS_ACCEPTED",
  "TRIP_STATUS_ARRIVED",
  "TRIP_STATUS_IN_PROGRESS",
  "TRIP_STATUS_COMPLETED",
  "TRIP_STATUS_CANCELLED",
  "TRIP_STATUS_UNMATCHED",
]).annotate({ identifier: "TripStatus" });
export type TripStatus = typeof TripStatus.Type;

/** Statuses a rider is still waiting through. */
export const isPending = (status: TripStatus): boolean =>
  status === "TRIP_STATUS_REQUESTED" || status === "TRIP_STATUS_OFFERED";

/** Statuses where a driver is on their way or the ride is under way. */
export const isUnderway = (status: TripStatus): boolean =>
  status === "TRIP_STATUS_ACCEPTED"
  || status === "TRIP_STATUS_ARRIVED"
  || status === "TRIP_STATUS_IN_PROGRESS";

/** Statuses nothing further will happen from. */
export const isFinished = (status: TripStatus): boolean =>
  status === "TRIP_STATUS_COMPLETED" || status === "TRIP_STATUS_CANCELLED";

/**
 * Money is minor units, always.
 *
 * An integer count of cents rather than a float of euros: 0.1 + 0.2 is not 0.3
 * in binary floating point, and a fare is not a place to discover that.
 */
export const Cents = Schema.Number.pipe(Schema.brand("Cents")).annotate({
  identifier: "Cents",
});
export type Cents = typeof Cents.Type;

/** Cents are an int64 on the wire; see Int64FromString. */
export const CentsFromString = Schema.String.pipe(
  Schema.decodeTo(Cents, {
    decode: SchemaGetter.transform((value) => Cents.make(Number(value))),
    encode: SchemaGetter.transform((value) => String(value)),
  }),
).annotate({ identifier: "CentsFromString" });

/** A quote for one vehicle class. */
export class FareQuote extends Schema.Class<FareQuote>("FareQuote")({
  fareId: FareId,
  packageSlug: Schema.String,
  totalCents: CentsFromString,
  surgeMultiplier: Schema.Number,
  expiresAt: Schema.String,
}) {}

/** A trip, as the API returns it. */
export class Trip extends Schema.Class<Trip>("Trip")({
  id: TripId,
  riderId: Schema.String,
  // Empty until a driver accepts. The API emits unpopulated fields rather than
  // omitting them, so this is "" rather than absent — which is why it is a
  // plain string and not an optional.
  driverId: Schema.String,
  status: TripStatus,
  pickup: Coordinate,
  dropoff: Coordinate,
  route: Route,
  totalCents: CentsFromString,
  createdAt: Schema.String,
  updatedAt: Schema.String,
}) {}

/** Whether a driver has been assigned. */
export const assignedDriver = (trip: Trip): DriverId | undefined =>
  trip.driverId === "" ? undefined : DriverId.make(trip.driverId);
