import { openOffers, secondsLeft, statusFor } from "@/drive/driver-status.js";
import { TripId } from "@surge/domain/api/Primitives";
import { Offer } from "@surge/domain/realtime/Wire";
import { Option, Schema } from "effect";
import { describe, expect, it } from "vitest";

const offer = (tripId: string, expiresAtMs: number) =>
  Schema.decodeSync(Offer)({
    tripId,
    driverId: "driver-1",
    riderId: "rider-1",
    pickupLat: 52.3791,
    pickupLng: 4.9003,
    replyCell: "871f1d489ffffff",
    expiresAtMs,
    dispatchedAtMs: 0,
    requestedAtMs: 0,
  });

describe("what the matcher is told", () => {
  it("follows the trip once one is accepted, whatever the toggle says", () => {
    // Offline with a trip is the case that matters: reporting `offline` or
    // `idle` mid-trip would put the driver back in the pool.
    expect(statusFor(false, Option.some("TRIP_STATUS_ACCEPTED"))).toBe("enroute_pickup");
    expect(statusFor(true, Option.some("TRIP_STATUS_ARRIVED"))).toBe("on_trip");
    expect(statusFor(false, Option.some("TRIP_STATUS_IN_PROGRESS"))).toBe("on_trip");
  });

  it("follows the toggle while the trip is still looking for a driver", () => {
    expect(statusFor(true, Option.some("TRIP_STATUS_REQUESTED"))).toBe("idle");
    expect(statusFor(false, Option.none())).toBe("offline");
  });
});

describe("offers on screen", () => {
  it("drops the expired and the answered, and keeps arrival order", () => {
    const offers = [offer("a", 2_000), offer("b", 10_000), offer("c", 10_000)];
    const answered = new Set([TripId.make("c")]);

    expect(openOffers(offers, answered, 5_000).map((open) => open.tripId)).toEqual(["b"]);
  });

  it("counts down in whole seconds and never below zero", () => {
    expect(secondsLeft(offer("a", 10_000), 8_500)).toBe(2);
    expect(secondsLeft(offer("a", 10_000), 12_000)).toBe(0);
  });
});
