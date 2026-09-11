import type { TripId } from "@surge/domain/api/Primitives";
import {
  type CityUpdate,
  type ClientMessage,
  ClientMessageFromJson,
  DriverPing,
  type DriverPosition,
  type DriverStatus,
  type FleetUpdate,
  type Offer,
  OfferReply,
  type PaymentsChange,
  type ServerMessage,
  ServerMessageFromJson,
  type TripUpdate,
  type Viewport,
} from "@surge/domain/realtime/Wire";
import {
  Clock,
  Config,
  Context,
  Duration,
  Effect,
  Layer,
  Option,
  PubSub,
  Queue,
  Ref,
  Result,
  Schedule,
  Schema,
  Stream,
  SubscriptionRef,
} from "effect";
import { Socket } from "effect/unstable/socket";
import { AuthToken } from "./AuthToken.js";

/**
 * The gateway's WebSocket, as an Effect `Stream`.
 *
 * This is the data plane. Everything a rider or driver does that is not a
 * request-response — a position report, a dispatched offer, the answer to one —
 * arrives here rather than over HTTP, and it goes to Go directly: no Node sits
 * in the path of ten thousand pings a second.
 *
 * Platform-free, like the rest of this package. It requires
 * `Socket.WebSocketConstructor` rather than reaching for a `WebSocket` global,
 * so `apps/web` provides `layerWebSocketConstructorGlobal` and `apps/mobile`
 * provides React Native's, and neither this file nor its callers change.
 *
 * The connection is owned by the service, not by whoever happens to be
 * subscribed. A React component mounting and unmounting must not open and close
 * a socket — the driver app is *always* connected while it is running, because
 * that is what being on shift means — so a supervised fiber holds one
 * connection for the layer's lifetime and fans frames out through a `PubSub`.
 */

/**
 * What to show a person about the connection.
 *
 * `Connected` is set by the gateway's `ServerWelcome` rather than by the
 * socket's own open event, and the difference matters: open means the TCP and
 * upgrade handshakes finished, welcome means the token verified and the
 * connection is in the registry that offers are routed through. A socket that
 * opened and was then closed for a bad token is not a connection anybody should
 * be told they have.
 */
export type ConnectionStatus = "Connecting" | "Connected";

export interface RealtimeService {
  /** Every frame the gateway sends. A fresh subscription per consumer, over one socket. */
  readonly messages: Stream.Stream<ServerMessage>;
  /** Just the dispatched offers — what `/drive` listens to. */
  readonly offers: Stream.Stream<Offer>;
  /**
   * Trips the caller is on, changing state — what `/ride` watches instead of
   * polling, and how `/drive` learns an accepted offer became its trip.
   */
  readonly tripUpdates: Stream.Stream<TripUpdate>;
  /** The console's live map, once a second while watching. */
  readonly fleet: Stream.Stream<FleetUpdate>;
  /** The assigned driver's position, once a second while following a trip. */
  readonly positions: Stream.Stream<DriverPosition>;
  /** The free cars and recent bookings a map shows, once a second while watching the city. */
  readonly city: Stream.Stream<CityUpdate>;
  /**
   * Something about the caller's money changed — what `/ride` watches for a
   * bank asking the rider to confirm, and `/drive/earnings` for a balance
   * moving.
   */
  readonly paymentsChanged: Stream.Stream<PaymentsChange>;
  /** For the reconnecting banner. Emits the current value on subscribe. */
  readonly status: Stream.Stream<ConnectionStatus>;

  /**
   * Report this driver's position.
   *
   * Owns the `(epoch, seq)` discipline so no call site has to: `epoch` is fixed
   * for the life of this service and `seq` increments per ping, which is
   * exactly what those two fields mean.
   */
  readonly ping: (position: {
    readonly lat: number;
    readonly lng: number;
    readonly heading: number;
    readonly speedMps: number;
    readonly status: DriverStatus;
  }) => Effect.Effect<void>;

  /**
   * Answer a dispatched offer.
   *
   * Takes the offer rather than its id, so the reply is routed by the cell the
   * offer arrived with — the shard that made the reservation is the only one
   * that can resolve it, and by the time a driver taps accept they may well be
   * somewhere else entirely.
   */
  readonly reply: (offer: Offer, accepted: boolean) => Effect.Effect<void>;

  /**
   * Watch the fleet through a viewport, or stop with `Option.none()`.
   *
   * Remembered and sent again after every reconnect. The gateway forgets a
   * subscription when the socket it arrived on closes — there is nothing else
   * it could do — so a console that lived through a gateway restart would
   * otherwise sit on its last frame, looking live and showing nothing new.
   */
  readonly watchFleet: (viewport: Option.Option<Viewport>) => Effect.Effect<void>;

  /** Follow a trip's driver, or stop with `Option.none()`. Re-sent after a reconnect, like `watchFleet`. */
  readonly followTrip: (tripId: Option.Option<TripId>) => Effect.Effect<void>;

  /**
   * Watch the city through a viewport, or stop with `Option.none()`. Any
   * signed-in caller may; re-sent after a reconnect, like `watchFleet`.
   */
  readonly watchCity: (viewport: Option.Option<Viewport>) => Effect.Effect<void>;

  /** The escape hatch, for anything the two helpers above do not cover. */
  readonly send: (message: ClientMessage) => Effect.Effect<void>;
}

/**
 * How many outbound frames may be waiting.
 *
 * Small, and dropping rather than blocking, which is the same decision the Go
 * gateway makes about its per-connection send queue and for a mirrored reason.
 * A position buffered through a twenty-second disconnect and delivered
 * afterwards is worse than no position: it reports where the driver *was*, and
 * ingest correctly rejects it as stale anyway, so the only thing the buffer
 * bought was memory and a delay behind it.
 *
 * An offer reply dropped this way is safe for a different reason — the offer
 * carries a TTL and the matcher re-dispatches when it expires, which is the
 * same path a driver who never answered takes.
 */
const OUTBOUND_CAPACITY = 32;

/**
 * How many inbound frames a slow subscriber may fall behind by.
 *
 * Sliding, so a component that stopped reading loses the oldest frames instead
 * of stalling the socket for everybody else. Same argument as the Go gateway's
 * slow-consumer eviction, one layer up.
 */
const INBOUND_CAPACITY = 64;

/**
 * Reconnect backoff.
 *
 * Capped, because an app left open overnight through a server deploy should not
 * come back forty minutes after the deploy finished. `Stream.retry` resets the
 * schedule once a connection emits a frame, and the gateway's first frame is
 * always `ServerWelcome`, so a connection that lasted a single second is
 * already enough to earn a fast retry — which is the behaviour you want when
 * the network is flapping and not when the gateway is refusing the token.
 */
const RECONNECT = Schedule.exponential(Duration.millis(500)).pipe(
  Schedule.modifyDelay(({ duration }) =>
    Effect.succeed(Duration.min(duration, Duration.seconds(20)))
  ),
  Schedule.jittered,
);

const decodeFrame = Schema.decodeEffect(ServerMessageFromJson);
const encodeFrame = Schema.encodeEffect(ClientMessageFromJson);

export class Realtime extends Context.Service<Realtime, RealtimeService>()("Realtime") {
  static layer: Layer.Layer<Realtime, never, Socket.WebSocketConstructor | AuthToken> = Layer
    .effect(Realtime)(
      Effect.gen(function*() {
        const baseUrl = yield* Config.nonEmptyString("SURGE_WS_URL").pipe(
          Config.withDefault("ws://localhost:8100/ws"),
        );
        const auth = yield* AuthToken;

        const outbound = yield* Queue.make<string>({
          capacity: OUTBOUND_CAPACITY,
          strategy: "dropping",
        });
        const inbound = yield* PubSub.sliding<ServerMessage>(INBOUND_CAPACITY);
        const connection = yield* SubscriptionRef.make<ConnectionStatus>("Connecting");

        /**
         * A browser cannot set headers on a WebSocket handshake, so the token
         * travels as a query parameter — which `authz.BearerToken` accepts
         * precisely for this case.
         *
         * Resolved per attempt rather than once, because a reconnect after a
         * long sleep is exactly when the previous token has expired. A caller
         * with no session still gets to try: the gateway answers 401, the
         * schedule backs off, and the UI shows "Connecting" rather than a
         * client-side error for what is a perfectly ordinary signed-out state.
         */
        const url = auth.get.pipe(
          Effect.map((token) => `${baseUrl}?token=${encodeURIComponent(token)}`),
          Effect.orElseSucceed(() => baseUrl),
        );

        const send = (message: ClientMessage): Effect.Effect<void> =>
          encodeFrame(message).pipe(
            Effect.flatMap((frame) => Queue.offer(outbound, frame)),
            Effect.asVoid,
            // The value was built from the schema's own types, so a failure
            // here is a bug in this file rather than something a caller could
            // respond to.
            Effect.catchTag("SchemaError", (error) => Effect.die(error)),
          );

        const watching = yield* Ref.make<Option.Option<Viewport>>(Option.none());
        const following = yield* Ref.make<Option.Option<TripId>>(Option.none());
        const watchingCity = yield* Ref.make<Option.Option<Viewport>>(Option.none());

        /** What this client has asked for, said again — on every welcome, since a new socket starts with nothing. */
        const resubscribe = Effect.gen(function*() {
          const viewport = yield* Ref.get(watching);
          if (Option.isSome(viewport)) {
            yield* send({ _tag: "ClientWatchFleet", viewport: viewport.value });
          }
          const trip = yield* Ref.get(following);
          if (Option.isSome(trip)) yield* send({ _tag: "ClientFollowTrip", tripId: trip.value });
          const city = yield* Ref.get(watchingCity);
          if (Option.isSome(city)) yield* send({ _tag: "ClientWatchCity", viewport: city.value });
        });

        const attempt = Stream.unwrap(
          Effect.map(Socket.makeWebSocket(url), (socket) =>
            Stream.fromQueue(outbound).pipe(
              Stream.pipeThroughChannel(Socket.toChannelString(socket)),
            )),
        );

        const frames = attempt.pipe(
          /**
           * A frame this build does not understand is dropped rather than
           * allowed to tear down a connection that is carrying live offers.
           * Contract drift is caught by `packages/domain/test/realtime`, which
           * holds both languages to one set of fixtures — that is the place to
           * find out, not a driver's phone mid-shift.
           */
          Stream.filterMapEffect((frame) =>
            decodeFrame(frame).pipe(
              Effect.map(Result.succeed),
              Effect.catchTag("SchemaError", () => Effect.succeed(Result.fail(frame))),
            )
          ),
          Stream.tap((message) =>
            message._tag === "ServerWelcome"
              ? SubscriptionRef.set(connection, "Connected").pipe(Effect.andThen(resubscribe))
              : Effect.void
          ),
          Stream.onError(() => SubscriptionRef.set(connection, "Connecting")),
          Stream.retry(RECONNECT),
        );

        yield* Effect.forkScoped(
          Stream.runForEach(frames, (message) => PubSub.publish(inbound, message)),
        );

        const messages = Stream.fromPubSub(inbound);

        /**
         * One epoch per service instance, which is what an epoch is: the client
         * session a sequence number is scoped to. Restarting the app starts a
         * new one, and because it is drawn from the clock it is larger than the
         * last, which is how a consumer holding sequence four hundred tells a
         * counter reset from four hundred consecutive reorderings.
         *
         * Milliseconds, where the Go simulator uses nanoseconds. Epochs are
         * only ever compared within one driver's own stream, so the unit only
         * has to be monotonic per client — and nanoseconds since 1970 is past
         * 2^53, which JavaScript cannot hold exactly and would round into a
         * value that no longer increases reliably.
         */
        const epoch = yield* Clock.currentTimeMillis;
        const sequence = yield* Ref.make(0);

        return {
          messages,
          offers: Stream.filterMap(
            messages,
            (message) =>
              message._tag === "Offer" ? Result.succeed(message.offer) : Result.fail(message),
          ),
          tripUpdates: Stream.filterMap(
            messages,
            (message) =>
              message._tag === "TripUpdated" ? Result.succeed(message.trip) : Result.fail(message),
          ),
          fleet: Stream.filterMap(
            messages,
            (message) =>
              message._tag === "FleetUpdate" ? Result.succeed(message.fleet) : Result.fail(message),
          ),
          positions: Stream.filterMap(
            messages,
            (message) =>
              message._tag === "DriverPosition"
                ? Result.succeed(message.position)
                : Result.fail(message),
          ),
          city: Stream.filterMap(
            messages,
            (message) =>
              message._tag === "CityUpdate" ? Result.succeed(message.city) : Result.fail(message),
          ),
          paymentsChanged: Stream.filterMap(
            messages,
            (message) =>
              message._tag === "PaymentsChanged"
                ? Result.succeed(message.payments)
                : Result.fail(message),
          ),
          status: SubscriptionRef.changes(connection),
          send,
          ping: (position) =>
            Effect.gen(function*() {
              const seq = yield* Ref.updateAndGet(sequence, (n) => n + 1);
              const sentAtMs = yield* Clock.currentTimeMillis;
              yield* send({
                _tag: "ClientPing",
                ping: new DriverPing({ epoch, seq, sentAtMs, ...position }),
              });
            }),
          reply: (offer, accepted) =>
            send({
              _tag: "ClientOfferReply",
              reply: new OfferReply({ tripId: offer.tripId, accepted }),
              replyCell: offer.replyCell,
            }),
          watchFleet: (viewport) =>
            Ref.set(watching, viewport).pipe(
              Effect.andThen(
                Option.match(viewport, {
                  onNone: () => send({ _tag: "ClientUnwatchFleet" }),
                  onSome: (value) => send({ _tag: "ClientWatchFleet", viewport: value }),
                }),
              ),
            ),
          followTrip: (tripId) =>
            Ref.set(following, tripId).pipe(
              Effect.andThen(
                Option.match(tripId, {
                  onNone: () => send({ _tag: "ClientUnfollowTrip" }),
                  onSome: (value) => send({ _tag: "ClientFollowTrip", tripId: value }),
                }),
              ),
            ),
          watchCity: (viewport) =>
            Ref.set(watchingCity, viewport).pipe(
              Effect.andThen(
                Option.match(viewport, {
                  onNone: () => send({ _tag: "ClientUnwatchCity" }),
                  onSome: (value) => send({ _tag: "ClientWatchCity", viewport: value }),
                }),
              ),
            ),
        };
      }),
      // A ConfigError means SURGE_WS_URL is set to something unusable, and
      // there is no client behaviour that recovers from not knowing where the
      // gateway is.
    ).pipe(Layer.orDie);
}
