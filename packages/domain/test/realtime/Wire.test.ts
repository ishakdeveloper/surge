import { DriverId, TripId } from "@surge/domain/api/Primitives";
import {
  CellId,
  ClientMessageFromJson,
  DriverPing,
  OfferReply,
  ServerMessageFromJson,
  Viewport,
} from "@surge/domain/realtime/Wire";
import { Effect, Schema } from "effect";
import * as fs from "node:fs";
import * as path from "node:path";
import { describe, expect, it } from "vitest";

/**
 * The TypeScript half of the WebSocket contract.
 *
 * `backend/shared/wire/wire_test.go` is the other half, and both read the same
 * files — that is the entire point. The REST surface is generated from
 * `proto/trip.proto` and drift is caught against the generated OpenAPI; this
 * transport is hand-written on both sides, which is the arrangement that
 * produced the drift in the starter this project borrows from, where the
 * TypeScript contracts declare trip events the Go does not have and the Go
 * declares a payment command set the TypeScript does not.
 *
 * Rename a field on either side and exactly one of these two suites fails.
 */
const testdata = path.join(
  import.meta.dirname,
  "..",
  "..",
  "..",
  "..",
  "backend",
  "shared",
  "wire",
  "testdata",
);

const fixture = (name: string): string => fs.readFileSync(path.join(testdata, name), "utf8");

const decodeServer = Schema.decodeUnknownEffect(ServerMessageFromJson);
const encodeClient = Schema.encodeEffect(ClientMessageFromJson);

describe("server messages", () => {
  it("decodes the welcome frame", async () => {
    const message = await Effect.runPromise(decodeServer(fixture("server_welcome.json")));
    expect(message._tag).toBe("ServerWelcome");
  });

  it("decodes an offer, ignoring the tag the payload repeats", async () => {
    const message = await Effect.runPromise(decodeServer(fixture("server_offer.json")));

    if (message._tag !== "Offer") {
      throw new Error(`expected an offer, got ${message._tag}`);
    }

    expect(message.offer.tripId).toBe("0f2a6c1e-9d4b-4a77-8c31-6b1e5a2d9f80");
    expect(message.offer.driverId).toBe("drv-000123");
    expect(message.offer.riderId).toBe("rider-000456");
    expect(message.offer.pickupLat).toBeCloseTo(52.3702, 4);
    expect(message.offer.pickupLng).toBeCloseTo(4.8952, 4);
    expect(message.offer.replyCell).toBe("871f1d492ffffff");
    expect(message.offer.expiresAtMs).toBe(1757512345000);
    expect(message.offer.dispatchedAtMs).toBe(1757512330000);
    expect(message.offer.requestedAtMs).toBe(1757512329412);
  });

  it("decodes a trip update", async () => {
    const message = await Effect.runPromise(decodeServer(fixture("server_trip_updated.json")));

    if (message._tag !== "TripUpdated") {
      throw new Error(`expected a trip update, got ${message._tag}`);
    }
    expect(message.trip.tripId).toBe("0f2a6c1e-9d4b-4a77-8c31-6b1e5a2d9f80");
    expect(message.trip.driverId).toBe("drv-000123");
    expect(message.trip.status).toBe("TRIP_STATUS_ACCEPTED");
  });

  /**
   * The status on a push is decoded by the same schema as the status on a REST
   * response — lifted out of the generated client, not restated — so a value the
   * API would reject is rejected here too, rather than rendered.
   */
  it("rejects a trip status the API does not have", async () => {
    const bogus = JSON.parse(fixture("server_trip_updated.json"));
    bogus.trip.status = "TRIP_STATUS_TELEPORTED";

    const result = await Effect.runPromise(Effect.result(decodeServer(JSON.stringify(bogus))));
    expect(result._tag).toBe("Failure");
  });

  it("decodes a fleet update in cell mode, with each cell's hexagon", async () => {
    const message = await Effect.runPromise(decodeServer(fixture("server_fleet_cells.json")));

    if (message._tag !== "FleetUpdate") {
      throw new Error(`expected a fleet update, got ${message._tag}`);
    }
    expect(message.fleet.mode).toBe("cells");
    expect(message.fleet.drivers).toEqual([]);
    expect(message.fleet.cells[0]?.cell).toBe("871f1d492ffffff");
    expect(message.fleet.cells[0]?.boundary).toHaveLength(6);
    expect(message.fleet.shards[0]).toMatchObject({
      partition: 7,
      instance: "matcher-a",
      ageMs: 340,
    });
    expect(message.fleet.stats.p99Ms).toBe(4400);
    expect(message.fleet.cells[0]?.multiplier).toBe(1.4);
    expect(message.fleet.stats).toMatchObject({ maxMultiplier: 1.4, surgingCells: 1 });
  });

  it("decodes a fleet update in driver mode", async () => {
    const message = await Effect.runPromise(decodeServer(fixture("server_fleet_drivers.json")));

    if (message._tag !== "FleetUpdate") {
      throw new Error(`expected a fleet update, got ${message._tag}`);
    }
    expect(message.fleet.mode).toBe("drivers");
    expect(message.fleet.cells).toEqual([]);
    expect(message.fleet.drivers[0]).toMatchObject({
      id: "drv-000123",
      heading: 137.5,
      status: "idle",
      reserved: false,
    });
  });

  it("decodes a driver's position for the rider following them", async () => {
    const message = await Effect.runPromise(decodeServer(fixture("server_driver_position.json")));

    if (message._tag !== "DriverPosition") {
      throw new Error(`expected a position, got ${message._tag}`);
    }
    expect(message.position.tripId).toBe("0f2a6c1e-9d4b-4a77-8c31-6b1e5a2d9f80");
    expect(message.position.driverId).toBe("drv-000123");
    expect(message.position.lat).toBeCloseTo(52.3711, 4);
    expect(message.position.etaSeconds).toBe(240);
  });

  /**
   * The payments service's doorbell: a hold needs the rider, a card was saved,
   * an earning moved. It names a trip when there is one, and an empty string —
   * not an absent field — when there is not.
   */
  it("decodes a payments change", async () => {
    const message = await Effect.runPromise(decodeServer(fixture("server_payments_changed.json")));

    if (message._tag !== "PaymentsChanged") {
      throw new Error(`expected a payments change, got ${message._tag}`);
    }
    expect(message.payments.tripId).toBe("0f2a6c1e-9d4b-4a77-8c31-6b1e5a2d9f80");
    expect(message.payments.atMs).toBe(1757512345000);

    const untied = await Effect.runPromise(
      decodeServer(`{"_tag":"PaymentsChanged","payments":{"tripId":"","atMs":1}}`),
    );
    expect(untied._tag).toBe("PaymentsChanged");
  });

  /**
   * Chat's doorbells carry no message: a conversation and its newest seq, a
   * read marker, or who is typing. The messages themselves are read over REST.
   */
  it("decodes chat's doorbells", async () => {
    const changed = await Effect.runPromise(decodeServer(fixture("server_chat_changed.json")));
    if (changed._tag !== "ChatChanged") {
      throw new Error(`expected a chat change, got ${changed._tag}`);
    }
    expect(changed.chat).toMatchObject({
      conversationId: "5b1d7e2a-3c4f-4e8a-9b6d-2f7c1a0e8d43",
      kind: "trip",
      tripId: "0f2a6c1e-9d4b-4a77-8c31-6b1e5a2d9f80",
      lastSeq: 7,
    });

    const read = await Effect.runPromise(decodeServer(fixture("server_chat_read.json")));
    if (read._tag !== "ChatRead") {
      throw new Error(`expected a read receipt, got ${read._tag}`);
    }
    expect(read.chatRead).toMatchObject({ userId: "drv-000123", seq: 7 });

    const typing = await Effect.runPromise(decodeServer(fixture("server_chat_typing.json")));
    if (typing._tag !== "ChatTyping") {
      throw new Error(`expected typing, got ${typing._tag}`);
    }
    expect(typing.chatTyping.userId).toBe("rider-000456");
  });

  it("rejects a conversation of a kind it does not know", async () => {
    const bogus = JSON.parse(fixture("server_chat_changed.json"));
    bogus.chat.kind = "group";

    const result = await Effect.runPromise(Effect.result(decodeServer(JSON.stringify(bogus))));
    expect(result._tag).toBe("Failure");
  });

  it("decodes an error frame", async () => {
    const message = await Effect.runPromise(decodeServer(fixture("server_error.json")));

    if (message._tag !== "ServerError") {
      throw new Error(`expected an error, got ${message._tag}`);
    }
    expect(message.error).toBe("token expired");
  });

  it("rejects a frame whose tag it does not know", async () => {
    const result = await Effect.runPromise(
      Effect.result(decodeServer(`{"_tag":"SurgeUpdated","multiplier":1.4}`)),
    );
    expect(result._tag).toBe("Failure");
  });

  /**
   * 64-bit fields are plain JSON numbers on this transport, unlike the REST
   * surface where proto3 quotes them. Getting that backwards is a decode error
   * on every single offer, so it is pinned rather than assumed.
   */
  it("reads 64-bit fields as numbers, not strings", () => {
    expect(JSON.parse(fixture("server_offer.json")).offer.expiresAtMs).toBeTypeOf("number");
  });
});

describe("client messages", () => {
  it("encodes a heartbeat the gateway recognises", async () => {
    const encoded = await Effect.runPromise(encodeClient({ _tag: "ClientHeartbeat" }));
    expect(JSON.parse(encoded)).toEqual(JSON.parse(fixture("client_heartbeat.json")));
  });

  it("encodes a ping the gateway recognises", async () => {
    const encoded = await Effect.runPromise(encodeClient({
      _tag: "ClientPing",
      ping: new DriverPing({
        epoch: 1757512300000,
        seq: 42,
        lat: 52.3702,
        lng: 4.8952,
        heading: 137.5,
        speedMps: 8.3,
        status: "idle",
        sentAtMs: 1757512329412,
      }),
    }));

    expect(JSON.parse(encoded)).toEqual(JSON.parse(fixture("client_ping.json")));
  });

  /**
   * The absence is the assertion. The gateway overwrites the driver id from the
   * verified token, because a client that could name its own could report
   * positions for somebody else and be dispatched their rides — so the schema
   * has no field for one, and this is what would notice somebody adding it.
   */
  it("cannot name a driver id", () => {
    expect(Object.keys(DriverPing.fields)).not.toContain("driverId");
  });

  it("encodes an offer reply carrying the cell it was given", async () => {
    const encoded = await Effect.runPromise(encodeClient({
      _tag: "ClientOfferReply",
      reply: new OfferReply({
        tripId: TripId.make("0f2a6c1e-9d4b-4a77-8c31-6b1e5a2d9f80"),
        accepted: true,
      }),
      replyCell: CellId.make("871f1d492ffffff"),
    }));

    expect(JSON.parse(encoded)).toEqual(JSON.parse(fixture("client_offer_reply.json")));
  });

  it("encodes a fleet watch with its viewport", async () => {
    const encoded = await Effect.runPromise(encodeClient({
      _tag: "ClientWatchFleet",
      viewport: new Viewport({ west: 4.85, south: 52.35, east: 4.95, north: 52.4, zoom: 13 }),
    }));
    expect(JSON.parse(encoded)).toEqual(JSON.parse(fixture("client_watch_fleet.json")));
  });

  it("encodes following a trip, and stopping both subscriptions", async () => {
    const follow = await Effect.runPromise(encodeClient({
      _tag: "ClientFollowTrip",
      tripId: TripId.make("0f2a6c1e-9d4b-4a77-8c31-6b1e5a2d9f80"),
    }));
    expect(JSON.parse(follow)).toEqual(JSON.parse(fixture("client_follow_trip.json")));

    const unwatch = await Effect.runPromise(encodeClient({ _tag: "ClientUnwatchFleet" }));
    expect(JSON.parse(unwatch)).toEqual(JSON.parse(fixture("client_unwatch_fleet.json")));
    const unfollow = await Effect.runPromise(encodeClient({ _tag: "ClientUnfollowTrip" }));
    expect(JSON.parse(unfollow)).toEqual(JSON.parse(fixture("client_unfollow_trip.json")));
  });

  it("rejects a position that is not on the planet", async () => {
    const result = await Effect.runPromise(
      Effect.result(
        Schema.decodeUnknownEffect(DriverPing)({
          epoch: 1,
          seq: 1,
          lat: 152.37,
          lng: 4.8952,
          heading: 0,
          speedMps: 0,
          status: "idle",
          sentAtMs: 0,
        }),
      ),
    );
    expect(result._tag).toBe("Failure");
  });
});

/**
 * Not drift, but the reason `replyCell` is branded at all. An offer's reply
 * must be routed to the shard that made the reservation rather than to the
 * driver's current cell, and the brand is what makes handing back the wrong
 * string a compile error instead of a dispatch that silently goes nowhere.
 */
it("threads an offer's reply cell back into the reply", async () => {
  const message = await Effect.runPromise(decodeServer(fixture("server_offer.json")));
  if (message._tag !== "Offer") throw new Error("expected an offer");

  const encoded = await Effect.runPromise(encodeClient({
    _tag: "ClientOfferReply",
    reply: new OfferReply({ tripId: message.offer.tripId, accepted: true }),
    replyCell: message.offer.replyCell,
  }));

  expect(JSON.parse(encoded).replyCell).toBe(
    JSON.parse(fixture("server_offer.json")).offer.replyCell,
  );
  expect(DriverId.make("drv-000123")).toBe(message.offer.driverId);
});
