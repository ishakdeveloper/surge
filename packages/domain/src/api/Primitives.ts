import { Schema, SchemaGetter } from "effect";

/**
 * What a `format` in the proto means in TypeScript.
 *
 * `proto/trip.proto` marks a field `format: "trip-id"` and the generated client
 * next door uses `TripId` for it. OpenAPI leaves `format` open on purpose, and
 * it is the only annotation that survives the whole chain — proto, document,
 * generated schema, path parameters included — which is what lets the semantic
 * type of a string be declared once, beside the field, in the file that already
 * owns the contract.
 *
 * This is the half a generator cannot write: brands and transforms are
 * decisions about how the client wants to hold a value, not facts the document
 * records.
 */

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

export const RiderId = Schema.String.pipe(Schema.brand("RiderId")).annotate({
  identifier: "RiderId",
});
export type RiderId = typeof RiderId.Type;

export const PaymentId = Schema.String.pipe(Schema.brand("PaymentId")).annotate({
  identifier: "PaymentId",
});
export type PaymentId = typeof PaymentId.Type;

export const WithdrawalId = Schema.String.pipe(Schema.brand("WithdrawalId")).annotate({
  identifier: "WithdrawalId",
});
export type WithdrawalId = typeof WithdrawalId.Type;

export const RefundId = Schema.String.pipe(Schema.brand("RefundId")).annotate({
  identifier: "RefundId",
});
export type RefundId = typeof RefundId.Type;

export const VehicleId = Schema.String.pipe(Schema.brand("VehicleId")).annotate({
  identifier: "VehicleId",
});
export type VehicleId = typeof VehicleId.Type;

export const DocumentId = Schema.String.pipe(Schema.brand("DocumentId")).annotate({
  identifier: "DocumentId",
});
export type DocumentId = typeof DocumentId.Type;

export const ConversationId = Schema.String.pipe(Schema.brand("ConversationId")).annotate({
  identifier: "ConversationId",
});
export type ConversationId = typeof ConversationId.Type;

export const MessageId = Schema.String.pipe(Schema.brand("MessageId")).annotate({
  identifier: "MessageId",
});
export type MessageId = typeof MessageId.Type;

/**
 * Anyone the auth service knows: the sender of a chat message is a rider, a
 * driver or somebody from support. `iam/Identity.ts` takes it from here, so
 * there is one brand for a person rather than one per file.
 */
export const UserId = Schema.String.pipe(Schema.brand("UserId")).annotate({
  identifier: "UserId",
});
export type UserId = typeof UserId.Type;

/**
 * The gateway's error codes, as a closed set a client can branch on.
 *
 * The gRPC code names rather than an invented per-endpoint vocabulary, because
 * that is what `rest.go` maps its statuses from — one translation table, not one
 * per handler.
 */
export const ErrorCode = Schema.Literals([
  "invalid_argument",
  "not_found",
  "already_exists",
  "failed_precondition",
  "aborted",
  "permission_denied",
  "unauthenticated",
  "deadline_exceeded",
  "unavailable",
  "rate_limited",
  "internal",
]).annotate({ identifier: "ErrorCode" });
export type ErrorCode = typeof ErrorCode.Type;

/**
 * proto3 JSON encodes `int64` and `uint64` as *strings*, because a JSON number
 * loses precision past 2^53. Every 64-bit field therefore arrives quoted, and a
 * schema expecting a number rejects it — which is exactly what happened, and is
 * the kind of thing only a test against the real server finds.
 *
 * Decoded once, here, rather than parsed in every component that renders a price
 * or a duration. `double` and `float` are unaffected: those stay JSON numbers.
 */
export const Int64FromString = Schema.String.pipe(
  Schema.decodeTo(Schema.Number, {
    decode: SchemaGetter.transform((value) => Number(value)),
    encode: SchemaGetter.transform((value) => String(value)),
  }),
).annotate({ identifier: "Int64FromString" });

/**
 * Money is minor units, always.
 *
 * An integer count of cents rather than a float of euros: 0.1 + 0.2 is not 0.3
 * in binary floating point, and a fare is not a place to discover that. Branded
 * so a cent count cannot be handed to something expecting euros.
 *
 * `format: "cents"` in the proto rather than letting grpc-gateway's `int64`
 * stand, because the brand is the point and `int64` would only get the decode.
 */
export const Cents = Schema.Number.pipe(Schema.brand("Cents")).annotate({
  identifier: "Cents",
});
export type Cents = typeof Cents.Type;

export const CentsFromString = Schema.String.pipe(
  Schema.decodeTo(Cents, {
    decode: SchemaGetter.transform((value) => Cents.make(Number(value))),
    encode: SchemaGetter.transform((value) => String(value)),
  }),
).annotate({ identifier: "CentsFromString" });
