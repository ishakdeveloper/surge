import { AuthToken } from "@/AuthToken.js";
import { Realtime } from "@/Realtime.js";
import { describe, expect, it } from "@effect/vitest";
import { Effect, Layer, Stream } from "effect";
import { Socket } from "effect/unstable/socket";

/**
 * The realtime client against the real gateway.
 *
 * `Realtime.test.ts` next door proves the reconnect, decode and sequencing
 * behaviour against a socket that never leaves the process. This proves the
 * parts only a real gateway can: that the token is accepted where a browser
 * must put it, that the welcome frame arrives, and that a ping sent from here
 * is a ping the gateway counts and forwards.
 *
 * Skips when nothing is running, so `pnpm test` stays useful on a bare machine.
 */
const authBase = process.env["AUTH_BASE_URL"] ?? "http://localhost:3200";
const gateway = process.env["SURGE_API_URL"] ?? "http://localhost:8100";
const gatewayMetrics = process.env["GATEWAY_METRICS_URL"] ?? "http://localhost:9104";
const webOrigin = process.env["WEB_URL"] ?? "http://localhost:5173";

const reachable = async (url: string): Promise<boolean> => {
  try {
    return (await fetch(url, { signal: AbortSignal.timeout(2000) })).ok;
  } catch {
    return false;
  }
};

const online = await reachable(`${gateway}/health`)
  && await reachable(`${authBase}/health`)
  && await reachable(`${gatewayMetrics}/metrics`);

/** A real driver, because only a driver's pings mean anything. */
const signUpDriver = async (): Promise<string> => {
  const email = `realtime-${Date.now()}@surge.test`;

  const signup = await fetch(`${authBase}/api/auth/sign-up/email`, {
    method: "POST",
    headers: { "content-type": "application/json", origin: webOrigin },
    body: JSON.stringify({
      email,
      password: "correct-horse-battery",
      name: "Realtime Test",
      role: "driver",
    }),
  });

  if (!signup.ok) {
    throw new Error(`sign-up failed with ${signup.status}: ${await signup.text()}`);
  }

  const cookie = signup.headers.getSetCookie()
    .map((entry) => entry.split(";")[0])
    .join("; ");

  const token = await fetch(`${authBase}/api/auth/token`, {
    headers: { cookie, origin: webOrigin },
  });
  const body = (await token.json()) as { token?: string; };
  if (typeof body.token !== "string") {
    throw new Error(`no token in response: ${JSON.stringify(body)}`);
  }
  return body.token;
};

/** Reads one Prometheus counter out of the gateway's own metrics. */
const counter = async (name: string): Promise<number> => {
  const body = await (await fetch(`${gatewayMetrics}/metrics`)).text();
  const line = body.split("\n").find((entry) => entry.startsWith(name));
  return line === undefined ? 0 : Number(line.slice(line.lastIndexOf(" ") + 1));
};

const token = online ? await signUpDriver() : "";

/** Wired the way `apps/web` does it: the platform layer only at the edge. */
const layer = Realtime.layer.pipe(
  Layer.provide([
    Socket.layerWebSocketConstructorGlobal,
    Layer.succeed(AuthToken)({
      get: Effect.succeed(token),
      identity: Effect.die("not needed here"),
      invalidate: Effect.void,
    }),
  ]),
);

describe.skipIf(!online)("Realtime against the running gateway", () => {
  /**
   * `it.live` rather than `it.effect`: this waits on a real socket, and a test
   * clock would sit at zero while the gateway takes its few milliseconds.
   */
  it.live("connects, and the gateway says so", () =>
    Effect.gen(function*() {
      const realtime = yield* Realtime;

      const states = yield* Stream.runCollect(Stream.take(realtime.status, 2)).pipe(
        Effect.timeout("10 seconds"),
      );

      // Connected is the gateway's welcome frame, which means the token
      // verified and the connection is in the registry offers are routed
      // through — not merely that a socket opened.
      expect(Array.from(states)).toEqual(["Connecting", "Connected"]);
    }).pipe(Effect.provide(layer)));

  it.live("sends a ping the gateway accepts", () =>
    Effect.gen(function*() {
      const realtime = yield* Realtime;

      yield* Stream.runDrain(Stream.take(realtime.status, 2)).pipe(Effect.timeout("10 seconds"));

      const before = yield* Effect.promise(() =>
        counter(`surge_gateway_inbound_total{tag="ClientPing"}`)
      );
      const rejected = yield* Effect.promise(() =>
        counter(`surge_gateway_rejected_total{reason="invalid_ping"}`)
      );

      yield* realtime.ping({
        lat: 52.3702,
        lng: 4.8952,
        heading: 137.5,
        speedMps: 8.3,
        status: "idle",
      });

      // The gateway counts on receipt, so a short wait is for the network
      // rather than for anything asynchronous on its side.
      yield* Effect.sleep("1 second");

      const after = yield* Effect.promise(() =>
        counter(`surge_gateway_inbound_total{tag="ClientPing"}`)
      );
      const rejectedAfter = yield* Effect.promise(() =>
        counter(`surge_gateway_rejected_total{reason="invalid_ping"}`)
      );

      expect(after).toBeGreaterThan(before);
      // The client omits the driver id and the gateway fills it in from the
      // token. If that ever stopped happening the ping would fail validation
      // rather than being silently attributed to nobody, and this is what
      // would notice.
      expect(rejectedAfter).toBe(rejected);
    }).pipe(Effect.provide(layer)));
});
