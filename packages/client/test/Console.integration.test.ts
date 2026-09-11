import { AuthToken } from "@/AuthToken.js";
import { Realtime } from "@/Realtime.js";
import { SurgeApi } from "@/SurgeApi.js";
import { describe, expect, it } from "@effect/vitest";
import { Viewport } from "@surge/domain/realtime/Wire";
import { Context, Effect, Layer, Option, Stream } from "effect";
import { FetchHttpClient } from "effect/unstable/http";
import { Socket } from "effect/unstable/socket";
import { execFileSync } from "node:child_process";
import { type AuthServer, outboxOpen, signInWithCode } from "./support/sign-in-with-code.js";

/**
 * The console, end to end: the matchers' frames → `fleet.frames` → the
 * gateway's fleet store → a viewport-shaped update on an ops socket, and the
 * simulator's knob through the generated client → the gateway's REST proxy →
 * simd over gRPC.
 *
 * And the refusals, which are the part a page cannot be trusted with: a rider
 * who asks for the fleet is told no by the socket, and a rider who asks for the
 * simulator is told no by the simulator.
 *
 * Skips when nothing is running. Needs the matcher and the simulator as well as
 * the gateway, because a frame only exists for a partition somebody owns, and
 * promoting an account to ops is a row in Postgres — the same update
 * `make grant-ops` makes.
 */
const authBase = process.env["AUTH_BASE_URL"] ?? "http://localhost:3200";
const gateway = process.env["SURGE_API_URL"] ?? "http://localhost:8100";
const auth: AuthServer = {
  base: authBase,
  origin: process.env["WEB_URL"] ?? "http://localhost:5273",
};

const reachable = async (url: string): Promise<boolean> => {
  try {
    return (await fetch(url, { signal: AbortSignal.timeout(2000) })).ok;
  } catch {
    return false;
  }
};

const online = await reachable(`${gateway}/health`)
  && await reachable(`${authBase}/health`)
  && await outboxOpen(auth);

const signUp = async (label: string): Promise<{ email: string; token: string; }> => {
  const email = `console-${label}-${Date.now()}@surge.test`;
  return { email, token: await signInWithCode(auth, email, "rider") };
};

/**
 * Nobody can sign up as ops — the sign-up form offers rider and driver, and the
 * auth service clamps anything else — so an ops account is promoted, then signs
 * in afresh, because the session it already has still carries the old role.
 */
const signUpOps = async (): Promise<string> => {
  const { email } = await signUp("ops");
  execFileSync("docker", [
    "exec",
    "surge-postgres",
    "psql",
    "-U",
    "surge",
    "-d",
    "surge_auth",
    "-c",
    `update "user" set role = 'ops' where email = '${email}'`,
  ]);
  return signInWithCode(auth, email);
};

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
  yield* Stream.runDrain(Stream.take(realtime.status, 2)).pipe(
    within("the gateway to welcome the socket", "30 seconds"),
  );
  return { api: Context.get(context, SurgeApi), realtime };
});

/** All of Amsterdam, zoomed out: counts per cell. */
const CITY = new Viewport({ west: 4.7, south: 52.28, east: 5.08, north: 52.45, zoom: 11 });
/** A few streets around Centraal, zoomed in: individual drivers. */
const CENTRE = new Viewport({ west: 4.87, south: 52.36, east: 4.93, north: 52.39, zoom: 15 });

describe.skipIf(!online)("the console, through every service", () => {
  it.live(
    "shows ops the fleet by cell, then by driver, and turns the simulator's knob",
    () =>
      Effect.gen(function*() {
        const ops = yield* actor(yield* Effect.promise(signUpOps));

        yield* ops.realtime.watchFleet(Option.some(CITY));
        const cells = yield* ops.realtime.fleet.pipe(
          Stream.filter((update) => update.mode === "cells" && update.shards.length > 0),
          Stream.runHead,
          within("a zoomed-out fleet update with live partitions", "30 seconds"),
        );
        const zoomedOut = Option.getOrThrow(cells);
        expect(zoomedOut.drivers).toEqual([]);
        expect(zoomedOut.cells.length).toBeGreaterThan(0);
        expect(zoomedOut.cells[0]!.boundary).toHaveLength(6);
        expect(zoomedOut.stats.drivers).toBeGreaterThan(0);

        yield* ops.realtime.watchFleet(Option.some(CENTRE));
        const drivers = yield* ops.realtime.fleet.pipe(
          Stream.filter((update) => update.mode === "drivers"),
          Stream.runHead,
          within("a zoomed-in fleet update", "15 seconds"),
        );
        const zoomedIn = Option.getOrThrow(drivers);
        expect(zoomedIn.cells).toEqual([]);
        for (const driver of zoomedIn.drivers) {
          expect(driver.lat).toBeGreaterThanOrEqual(CENTRE.south);
          expect(driver.lng).toBeLessThanOrEqual(CENTRE.east);
        }

        const { simulator } = yield* ops.api.simulator.get();
        expect(simulator.drivers).toBeGreaterThan(0);
        expect(simulator.targetPingsPerSecond).toBeCloseTo(
          simulator.drivers / (simulator.pingIntervalMs / 1000),
        );

        // Asked for exactly what it is already doing, so the rest of the stack
        // is not disturbed — what is under test is that the PUT reaches simd.
        const rescaled = yield* ops.api.simulator.configure({
          payload: { drivers: simulator.drivers, pingIntervalMs: simulator.pingIntervalMs },
        });
        expect(rescaled.simulator.drivers).toBe(simulator.drivers);
      }).pipe(Effect.scoped),
    90_000,
  );

  it.live("refuses a rider both the fleet and the simulator", () =>
    Effect.gen(function*() {
      const rider = yield* actor((yield* Effect.promise(() => signUp("rider"))).token);

      yield* rider.realtime.watchFleet(Option.some(CITY));
      const refusal = yield* rider.realtime.messages.pipe(
        Stream.filter((message) =>
          message._tag === "ServerError" || message._tag === "FleetUpdate"
        ),
        Stream.runHead,
        within("the gateway to answer a rider's fleet watch", "15 seconds"),
      );
      expect(Option.getOrThrow(refusal)).toMatchObject({ _tag: "ServerError" });

      const denied = yield* Effect.flip(rider.api.simulator.get());
      expect(denied).toMatchObject({ error: { code: "permission_denied" } });
    }).pipe(Effect.scoped), 60_000);
});
