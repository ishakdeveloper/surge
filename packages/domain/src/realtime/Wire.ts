import { Schema } from "effect";
import { DriverId, TripId } from "../api/Primitives.js";

/**
 * The gateway's WebSocket protocol, mirroring `backend/shared/wire`.
 *
 * Two things about this wire are worth stating up front, because both differ
 * from the HTTP contract in `../api/SurgeApi.ts`:
 *
 * The envelope is tagged at the top level and carries its payload in a *named
 * field* — `{ "_tag": "Offer", "offer": { ... } }` rather than a flattened
 * struct. That is what Go's `encoding/json` produces from a struct with one
 * pointer per variant, and modelling it faithfully is cheaper than asking the
 * hot path to reshape itself for the client.
 *
 * And 64-bit fields are plain JSON numbers here, not strings. The REST API is
 * protobuf-derived, where proto3 JSON quotes `int64` because a JSON number
 * loses precision past 2^53; this transport is `encoding/json`, which does not.
 * Every 64-bit value on this wire is a Unix-millisecond timestamp or a
 * per-session counter, both of which are three orders of magnitude below that
 * limit, so a `Schema.Number` is exact rather than merely convenient.
 */

/**
 * An H3 cell as a hex string.
 *
 * Branded because of `replyCell`, below: routing an offer's answer to the wrong
 * cell delivers it to a shard holding no reservation for it, and the brand is
 * what makes "return the cell you were given" a thing the compiler checks
 * rather than a thing a comment asks for.
 */
export const CellId = Schema.String.pipe(Schema.brand("CellId")).annotate({
  identifier: "CellId",
});
export type CellId = typeof CellId.Type;

/**
 * Where a driver is in the trip lifecycle.
 *
 * `offline` still emits pings, because the console shows supply rather than
 * only availability. Only `idle` is a driver the matcher may offer work to.
 */
export const DriverStatus = Schema.Literals([
  "offline",
  "idle",
  "enroute_pickup",
  "on_trip",
]).annotate({ identifier: "DriverStatus" });
export type DriverStatus = typeof DriverStatus.Type;

/**
 * One GPS report, and the highest-volume message in the system by two orders of
 * magnitude.
 *
 * `driverId` is deliberately absent, though `wire.DriverPing` in Go has one:
 * the gateway overwrites it from the verified token before producing, because
 * a client that could name its own driver id could report positions for
 * somebody else — and be dispatched their rides. Leaving the field out of the
 * schema makes that unrepresentable here rather than merely ignored there.
 */
export class DriverPing extends Schema.Class<DriverPing>("DriverPing")({
  /**
   * The client session this `seq` belongs to.
   *
   * A sequence number means nothing outside its session: restart the client and
   * the counter returns to 1, which to a consumer holding sequence 400 looks
   * like four hundred consecutive reorderings, and every subsequent ping is
   * rejected as stale. Comparing `(epoch, seq)` is what makes a counter reset a
   * new session instead of an eternal one.
   */
  epoch: Schema.Number,
  /** Monotonic within `epoch`. */
  seq: Schema.Number,

  lat: Schema.Number.check(Schema.isBetween({ minimum: -90, maximum: 90 })),
  lng: Schema.Number.check(Schema.isBetween({ minimum: -180, maximum: 180 })),

  /** Degrees clockwise from north, so the map can point the marker. */
  heading: Schema.Number,
  /** Metres per second. */
  speedMps: Schema.Number,

  status: DriverStatus,

  /**
   * When the client emitted this, in Unix milliseconds.
   *
   * The end-to-end latency histogram is `received - sentAtMs`, which is the
   * number that says whether ingest is keeping up. Consumer lag says how many
   * records are outstanding; this says how stale the map is, and the two
   * diverge exactly when it matters.
   */
  sentAtMs: Schema.Number,
}) {}

/**
 * A dispatched ride offer — what a driver is actually shown.
 *
 * Carried on `ws.push` keyed by driver id rather than on `geo.events`, because
 * it is addressed to a person rather than to a place.
 */
export class Offer extends Schema.Class<Offer>("Offer")({
  tripId: TripId,
  driverId: DriverId,
  riderId: Schema.String,

  pickupLat: Schema.Number,
  pickupLng: Schema.Number,

  /**
   * Where the answer must be sent: the cell of the shard that made the
   * reservation, **not** the driver's current cell.
   *
   * The distinction matters because a driver moves. By the time they tap accept
   * they may be in a different cell owned by a different instance, and routing
   * the reply by their current position would deliver it to a shard holding no
   * reservation for it. The reservation's owner is the only shard that can
   * resolve the offer, so the offer carries its own return address.
   */
  replyCell: CellId,

  expiresAtMs: Schema.Number,
  dispatchedAtMs: Schema.Number,
  /**
   * When the rider asked, carried the whole way through so match latency is
   * measured end to end rather than per hop.
   */
  requestedAtMs: Schema.Number,
}) {}

/** A driver's answer to a dispatched offer. `driverId` is absent for the same reason it is on {@link DriverPing}. */
export class OfferReply extends Schema.Class<OfferReply>("OfferReply")({
  tripId: TripId,
  accepted: Schema.Boolean,
}) {}

/**
 * Client to server.
 *
 * `ClientHeartbeat` exists so an otherwise silent connection is marked as seen:
 * the gateway evicts idle sockets after ninety seconds, and a rider waiting for
 * a driver is waiting rather than gone.
 */
export const ClientMessage = Schema.Union([
  Schema.TaggedStruct("ClientHeartbeat", {}).annotate({ identifier: "ClientHeartbeat" }),
  Schema.TaggedStruct("ClientPing", { ping: DriverPing }).annotate({ identifier: "ClientPing" }),
  Schema.TaggedStruct("ClientOfferReply", {
    reply: OfferReply,
    replyCell: CellId,
  }).annotate({ identifier: "ClientOfferReply" }),
]).annotate({ identifier: "ClientMessage" });
export type ClientMessage = typeof ClientMessage.Type;

/**
 * Server to client.
 *
 * `ServerWelcome` is the first frame on every accepted connection, which makes
 * it the application-level "the socket is up" signal — the transport's own open
 * event says the TCP handshake finished, this says the gateway verified the
 * token and registered the connection.
 *
 * `ServerError` carries a reason so a client can tell "your token expired" from
 * "the network dropped", which are the same event to a socket and completely
 * different events to a person.
 */
export const ServerMessage = Schema.Union([
  Schema.TaggedStruct("ServerWelcome", {}).annotate({ identifier: "ServerWelcome" }),
  Schema.TaggedStruct("Offer", { offer: Offer }).annotate({ identifier: "OfferMessage" }),
  Schema.TaggedStruct("ServerError", { error: Schema.String }).annotate({
    identifier: "ServerError",
  }),
]).annotate({ identifier: "ServerMessage" });
export type ServerMessage = typeof ServerMessage.Type;

/**
 * The framing. Text frames, so a partition or a socket is readable while
 * debugging — the same trade the Kafka topics make, and for the same reason.
 */
export const ServerMessageFromJson = Schema.fromJsonString(ServerMessage).annotate({
  identifier: "ServerMessageFromJson",
});

export const ClientMessageFromJson = Schema.fromJsonString(ClientMessage).annotate({
  identifier: "ClientMessageFromJson",
});
