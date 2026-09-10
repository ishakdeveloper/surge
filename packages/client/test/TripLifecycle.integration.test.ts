import { AuthToken } from "@/AuthToken.js";
import { Realtime } from "@/Realtime.js";
import { SurgeApi } from "@/SurgeApi.js";
import { describe, expect, it } from "@effect/vitest";
import { TripId } from "@surge/domain/api/Primitives";
import type { Offer, TripUpdate } from "@surge/domain/realtime/Wire";
import { Context, Deferred, Effect, Exit, Fiber, Layer, Queue, Schedule, Stream } from "effect";
import { FetchHttpClient } from "effect/unstable/http";
import { Socket } from "effect/unstable/socket";

/**
 * One ride, end to end, through every service, with a real rider and a real
 * driver on the other side of it.
 *
 * rider books over REST → trip service → `geo.events` → matcher → offer on
 * `ws.push` → gateway → the driver's socket → accept → matcher → `trip.events`
 * → trip service → `TripUpdated` on `ws.push` → both sockets. Then the driver
 * arrives, starts and completes over REST, and the rider watches each one land
 * without asking.
 *
 * Nothing here is mocked, which is the point: every seam this crosses has its
 * own unit test, and none of them can catch the envelope one service writes not
 * being the envelope another reads.
 *
 * Skips when nothing is running. Needs ingest and matcher as well as the
 * gateway and trip service, because an offer only reaches a driver the matcher
 * has indexed.
 */
const authBase = process.env["AUTH_BASE_URL"] ?? "http://localhost:3200";
const gateway = process.env["SURGE_API_URL"] ?? "http://localhost:8100";
const webOrigin = process.env["WEB_URL"] ?? "http://localhost:5173";

const reachable = async (url: string): Promise<boolean> => {
  try {
    return (await fetch(url, { signal: AbortSignal.timeout(2000) })).ok;
  } catch {
    return false;
  }
};

const online = await reachable(`${gateway}/health`) && await reachable(`${authBase}/health`);

const signUp = async (role: "rider" | "driver"): Promise<string> => {
  const signup = await fetch(`${authBase}/api/auth/sign-up/email`, {
    method: "POST",
    headers: { "content-type": "application/json", origin: webOrigin },
    body: JSON.stringify({
      email: `lifecycle-${role}-${Date.now()}@surge.test`,
      password: "correct-horse-battery",
      name: `Lifecycle ${role}`,
      role,
    }),
  });
  if (!signup.ok) throw new Error(`sign-up failed with ${signup.status}: ${await signup.text()}`);

  const cookie = signup.headers.getSetCookie().map((entry) => entry.split(";")[0]).join("; ");
  const body = (await (await fetch(`${authBase}/api/auth/token`, {
    headers: { cookie, origin: webOrigin },
  })).json()) as { token?: string; };
  if (typeof body.token !== "string") throw new Error("no token");
  return body.token;
};

/**
 * Both clients, wired the way `apps/web` wires one: the platform layers at the
 * edge and nothing else. Two people means two of everything — two token caches,
 * two sockets — so each actor gets its own built context rather than sharing
 * the test's.
 */
const clientFor = (token: string) =>
  Layer.mergeAll(SurgeApi.layer, Realtime.layer).pipe(
    Layer.provide(
      Layer.succeed(AuthToken)({
        get: Effect.succeed(token),
        identity: Effect.die("not needed here"),
        invalidate: Effect.void,
      }),
    ),
    Layer.provide([FetchHttpClient.layer, Socket.layerWebSocketConstructorGlobal]),
  );

/**
 * A bounded wait that says what it was waiting for.
 *
 * A bare `TimeoutError` names nothing, and this test waits on several things
 * across five services — "it timed out" is not a finding. The bounds are
 * generous on purpose: this asserts that every step happens, and in order, not
 * how fast. On one laptop a starved Redpanda can cost the matcher its
 * partitions, and an offer that arrives late after it takes them back is the
 * system recovering correctly rather than failing.
 */
const within =
  (step: string, duration: `${number} seconds`) => <A, E, R>(self: Effect.Effect<A, E, R>) =>
    self.pipe(
      Effect.timeoutOrElse({
        duration,
        orElse: () => Effect.die(new Error(`timed out after ${duration} waiting for: ${step}`)),
      }),
    );

const actor = Effect.fnUntraced(function*(token: string) {
  const context = yield* Layer.build(clientFor(token));
  const realtime = Context.get(context, Realtime);
  // Connected means the gateway verified the token and registered the socket,
  // which is what a push needs before it can be delivered.
  yield* Stream.runDrain(Stream.take(realtime.status, 2)).pipe(
    within("the gateway to welcome the socket", "30 seconds"),
  );
  return { api: Context.get(context, SurgeApi), realtime };
});

/**
 * Sloterdijk station, where the driver waits exactly.
 *
 * Deliberately not the Centraal Station point the other integration tests book
 * from. The suite runs concurrently, and a booking there by another test is a
 * ride the matcher offers to whichever driver is nearest — this one — which
 * holds the driver reserved for somebody else's trip until the offer expires.
 */
const PICKUP = { lat: 52.3889, lng: 4.8377 };
const DROPOFF = { lat: 52.3868, lng: 4.8752 };

const tokens = online
  ? { rider: await signUp("rider"), driver: await signUp("driver") }
  : { rider: "", driver: "" };

describe.skipIf(!online)("a trip, end to end", () => {
  it.live(
    "is booked, matched, driven and completed, and the rider sees every step",
    () =>
      Effect.gen(function*() {
        const rider = yield* actor(tokens.rider);
        const driver = yield* actor(tokens.driver);

        // The driver reports from exactly the pickup, every two seconds, for the
        // whole test. Two thousand simulated drivers are also idle in this city
        // and the matcher ranks by distance, so standing on the spot is what makes
        // the offer come to this driver rather than to one of them.
        yield* driver.realtime.ping({ ...PICKUP, heading: 0, speedMps: 0, status: "idle" }).pipe(
          Effect.repeat(Schedule.spaced("2 seconds")),
          Effect.forkScoped,
        );

        // Subscribed before booking, or the REQUESTED push lands before anyone
        // is listening for it.
        const seen: Array<TripUpdate["status"]> = [];
        const completed = yield* Deferred.make<void>();
        yield* rider.realtime.tripUpdates.pipe(
          Stream.runForEach((update) =>
            Effect.sync(() => seen.push(update.status)).pipe(
              Effect.andThen(
                update.status === "TRIP_STATUS_COMPLETED"
                  ? Deferred.succeed(completed, undefined)
                  : Effect.void,
              ),
            )
          ),
          Effect.forkScoped,
        );

        // Every offer from before booking is queued rather than read live: the
        // PubSub has no replay, and the offer for this trip can land before the
        // booking's HTTP response does.
        const inbox = yield* Queue.make<Offer>();
        yield* driver.realtime.offers.pipe(
          Stream.runForEach((incoming) => Queue.offer(inbox, incoming)),
          Effect.forkScoped,
        );

        const driverAccepted = yield* driver.realtime.tripUpdates.pipe(
          Stream.filter((update) => update.status === "TRIP_STATUS_ACCEPTED"),
          Stream.take(1),
          Stream.runCollect,
          Effect.forkScoped,
        );

        // Long enough for the pings to reach the matcher's index through ingest.
        yield* Effect.sleep("3 seconds");

        const preview = yield* rider.api.trips.preview({
          payload: { pickup: PICKUP, dropoff: DROPOFF },
        });
        const { trip } = yield* rider.api.trips.create({
          payload: { fareId: preview.fares[0]!.fareId, idempotencyKey: `lifecycle-${Date.now()}` },
        });

        // A real driver answers every offer. Declining the ones that are not this
        // trip — another test's ride, a simulated rider's — releases the
        // reservation now instead of when the offer times out, which is what
        // stops a stray request from starving this one.
        const offer = yield* Effect.gen(function*() {
          while (true) {
            const next = yield* Queue.take(inbox);
            if (next.tripId === trip.id) return next;
            yield* driver.realtime.reply(next, false);
          }
        }).pipe(within("the matcher to offer this trip to this driver", "45 seconds"));
        yield* driver.realtime.reply(offer, true);

        const [accepted] = Array.from(
          yield* Fiber.join(driverAccepted).pipe(
            within("the accepted trip to reach the driver", "45 seconds"),
          ),
        );
        expect(accepted?.tripId).toBe(trip.id);

        // A driver reloading mid-ride finds the trip they are on.
        const listed = yield* driver.api.trips.list({ query: {} });
        expect(listed.trips.map((listedTrip) => listedTrip.id)).toContain(trip.id);

        // Only the assigned driver moves a trip forward, and the rider is not it.
        const riderTried = yield* Effect.exit(
          rider.api.trips.arrive({ params: { tripId: trip.id } }),
        );
        expect(Exit.isFailure(riderTried)).toBe(true);

        const tripId = TripId.make(trip.id);
        yield* driver.api.trips.arrive({ params: { tripId } });
        yield* driver.api.trips.start({ params: { tripId } });
        const done = yield* driver.api.trips.complete({ params: { tripId } });
        expect(done.trip.status).toBe("TRIP_STATUS_COMPLETED");

        yield* Deferred.await(completed).pipe(
          within("the rider to see the trip complete", "30 seconds"),
        );
        expect(seen).toEqual([
          "TRIP_STATUS_REQUESTED",
          "TRIP_STATUS_ACCEPTED",
          "TRIP_STATUS_ARRIVED",
          "TRIP_STATUS_IN_PROGRESS",
          "TRIP_STATUS_COMPLETED",
        ]);
      }),
    180_000,
  );
});
