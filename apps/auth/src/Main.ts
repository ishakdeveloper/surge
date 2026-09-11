import { NodeHttpServer, NodeRuntime } from "@effect/platform-node";
import { PgLive } from "@surge/database/PgLive";
import { PgPool } from "@surge/database/PgPool";
import { Config, Effect, Layer } from "effect";
import { HttpRouter } from "effect/unstable/http";
import { RateLimiter } from "effect/unstable/persistence";
import * as Http from "node:http";
import { DevOutbox } from "./dev/DevOutbox.js";
import { DevOutboxHttp } from "./dev/DevOutboxHttp.js";
import { Mailer } from "./email/Mailer.js";
import { HealthHttp } from "./health/HealthHttp.js";
import { Auth } from "./iam/Auth.js";
import { AuthHttp } from "./iam/AuthHttp.js";
import { Sms } from "./sms/Sms.js";
import { TelemetryLive } from "./Telemetry.js";

/**
 * The auth service, and only the auth service.
 *
 * Every product API in this system is Go. This process exists because
 * better-auth is worth keeping and not worth porting — sessions, OAuth, email
 * and phone one-time codes, CSRF and cookie scoping are a multi-week detour
 * that teaches nothing the rest of the project is about.
 *
 * It is deliberately off the hot path. Nothing calls it per request: the
 * browser trades its session cookie for a short-lived JWT at
 * `/api/auth/token`, and Go verifies that signature locally against
 * `/api/auth/jwks`. So this process can be slow, or briefly down, without any
 * ride in progress noticing.
 *
 * Resist adding endpoints here. A product endpoint added to this file is a
 * product endpoint in the wrong language.
 */

/**
 * The browser talks to this service directly, from the web app's origin.
 *
 * That makes every request cross-origin, so the browser demands CORS before it
 * will send one — and `credentials` because the session cookie is the whole
 * point. `WEB_URL` is the only origin allowed: it is already the trusted origin
 * better-auth checks, so widening this would let a request through that
 * better-auth then refuses anyway.
 */
const CorsLive = Layer.unwrap(
  Effect.gen(function*() {
    const webUrl = yield* Config.nonEmptyString("WEB_URL").pipe(
      Config.withDefault("http://localhost:5273"),
    );

    return HttpRouter.cors({ allowedOrigins: [webUrl], credentials: true });
  }),
);

const Routes = Layer.mergeAll(AuthHttp, HealthHttp, DevOutboxHttp, CorsLive);

const HttpLive = Layer.unwrap(
  Effect.gen(function*() {
    const port = yield* Config.port("PORT").pipe(Config.withDefault(3000));

    // `serve` materialises the requirements the route handlers declared, so the
    // application layers are provided here rather than to `Routes`. `PgPool` is
    // provided once for the whole tree, so layer memoisation guarantees the
    // SqlClient and better-auth share a single connection pool.
    return HttpRouter.serve(Routes).pipe(
      Layer.provide(Auth.layer),
      // Swap `layerStoreMemory` for `layerStoreRedis` to share limits across workers.
      Layer.provide(RateLimiter.layer),
      Layer.provide(RateLimiter.layerStoreMemory),
      Layer.provide(PgLive),
      // The outbox is one layer value referenced three times, so it is built
      // once: the senders record into the same store the route reads.
      Layer.provide(Mailer.layer.pipe(Layer.provide(DevOutbox.layer))),
      Layer.provide(Sms.layer.pipe(Layer.provide(DevOutbox.layer))),
      Layer.provide(DevOutbox.layer),
      Layer.provide(PgPool.layer),
      Layer.provide(NodeHttpServer.layer(() => Http.createServer(), { port })),
    );
  }),
);

/**
 * Telemetry is provided to the running effect rather than into the layer graph.
 *
 * The tracer has to be installed in the context the application *runs in* — a
 * layer nothing names as a dependency is not built, so providing it inside the
 * graph silently did nothing at all.
 */
NodeRuntime.runMain(Layer.launch(HttpLive).pipe(Effect.provide(TelemetryLive)));
