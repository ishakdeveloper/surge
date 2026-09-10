import { Schema } from "effect";
import { HttpApi, HttpApiEndpoint, HttpApiGroup } from "effect/unstable/httpapi";
import { Coordinate, FareId, FareQuote, Route, Trip, TripId } from "../trip/Trip.js";

/**
 * The Go API, as this codebase consumes it.
 *
 * The server is Go and the REST surface is generated from `proto/trip.proto` by
 * grpc-gateway, so this is a *client-side declaration* of that contract —
 * `HttpApiBuilder` is never used, because no TypeScript implements it. What it
 * buys is a typed client and runtime decoding at the boundary, which is the
 * difference between a wrong response failing here with a readable error and
 * failing three components later as `undefined is not an object`.
 *
 * ## Why this is hand-written when a generator exists
 *
 * `@effect/openapi-generator` reads the same Swagger document and emits a
 * working `HttpApi`; `make proto` runs it, and the output is committed at
 * `../../test/api/generated.ts`. It is not used as the client, for three
 * reasons that all show up at every call site rather than here:
 *
 * - **Everything would be optional.** proto3 has no required fields, so every
 *   property in the Swagger is optional and `trip.id` decodes as
 *   `string | undefined`. The gateway sets `EmitUnpopulated: true`, so an
 *   unassigned driver arrives as `""` rather than absent — a fact the document
 *   does not record and a generator cannot infer.
 * - **Ids would be bare strings.** `TripId` and `FareId` are branded here, which
 *   is what makes passing a fare id where a trip id belongs a compile error.
 * - **`int64` would stay a string.** proto3 JSON quotes 64-bit numbers, so
 *   `totalCents` is `"1627"` on the wire. `Int64FromString` decodes it once,
 *   here, instead of in every component that renders a price.
 *
 * What the generator is used for instead is `test/api/ApiContract.test.ts`,
 * which diffs this file against its output: routes, methods, parameters and
 * every reachable field name. Drift becomes a failing test rather than a 404.
 */

/**
 * The one error shape the gateway emits.
 *
 * A stable `code` clients branch on, and a `message` for a human. Internal
 * failures are deliberately vague — the detail stays in the trace rather than
 * travelling to a browser.
 */
export class ApiError extends Schema.Class<ApiError>("ApiError")({
  error: Schema.Struct({
    code: Schema.Literals([
      "invalid_argument",
      "not_found",
      "already_exists",
      "failed_precondition",
      "permission_denied",
      "unauthenticated",
      "deadline_exceeded",
      "unavailable",
      "rate_limited",
      "internal",
    ]),
    message: Schema.String,
  }),
}) {}

/** A fare quote is not a booking; both are needed to make one. */
export class PreviewResponse extends Schema.Class<PreviewResponse>("PreviewResponse")({
  fares: Schema.Array(FareQuote),
  route: Route,
}) {}

export class TripResponse extends Schema.Class<TripResponse>("TripResponse")({
  trip: Trip,
}) {}

export class TripListResponse extends Schema.Class<TripListResponse>("TripListResponse")({
  // Always present, never absent: the gateway emits unpopulated fields, so a
  // rider with no history gets `{"trips":[],"nextPageToken":""}` rather than an
  // empty object. Declaring these optional would be describing a server that
  // does not exist.
  trips: Schema.Array(Trip),
  // Empty when there are no more pages. Opaque — a client that decodes it is a
  // client that breaks when the ordering changes.
  nextPageToken: Schema.String,
}) {}

export const TripsGroup = HttpApiGroup.make("trips")
  .add(
    HttpApiEndpoint.post("preview", "/v1/trips:preview", {
      payload: Schema.Struct({ pickup: Coordinate, dropoff: Coordinate }),
      success: PreviewResponse,
      error: ApiError,
    }),
  )
  .add(
    HttpApiEndpoint.post("create", "/v1/trips", {
      payload: Schema.Struct({ fareId: FareId }),
      // The key is a header, not a body field, because it describes the
      // request rather than the trip — and because a proxy or a retrying
      // client can see a header without parsing a body.
      headers: { "idempotency-key": Schema.String },
      success: TripResponse,
      error: ApiError,
    }),
  )
  .add(
    HttpApiEndpoint.get("get", "/v1/trips/:tripId", {
      params: { tripId: TripId },
      success: TripResponse,
      error: ApiError,
    }),
  )
  .add(
    HttpApiEndpoint.get("list", "/v1/trips", {
      query: {
        pageSize: Schema.optional(Schema.String),
        pageToken: Schema.optional(Schema.String),
        status: Schema.optional(Schema.String),
      },
      success: TripListResponse,
      error: ApiError,
    }),
  )
  .add(
    // The path-segment binding rather than the `:cancel` one. Both reach the
    // same handler; this is the one Effect's router can express.
    HttpApiEndpoint.post("cancel", "/v1/trips/:tripId/cancel", {
      params: { tripId: TripId },
      payload: Schema.Struct({ reason: Schema.String }),
      success: TripResponse,
      error: ApiError,
    }),
  );

export const SurgeApi = HttpApi.make("surge").add(TripsGroup);
