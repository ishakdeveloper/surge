import { SurgeApi as ApiDefinition } from "@surge/domain/api/SurgeApi";
import { Config, Context, Effect, Layer, Schedule } from "effect";
import { HttpClient } from "effect/unstable/http";
import { HttpApi, HttpApiClient } from "effect/unstable/httpapi";
import { AuthToken, bearer } from "./AuthToken.js";

/**
 * The Go API, as a service.
 *
 * Platform-free by rule: it declares `HttpClient` as a requirement and never
 * imports `@effect/platform-browser`, a Node built-in or a DOM global. Each app
 * provides the platform layer at its edge — `FetchHttpClient` in the browser,
 * React Native's equivalent in `apps/mobile` — which is what makes the mobile
 * app a new entry point rather than a rewrite.
 */
/**
 * The generated client's shape, derived from the API declaration.
 *
 * Extracted from the HttpApi's own type parameter rather than restated, so
 * adding an endpoint to the contract widens this automatically and a call to a
 * method that does not exist is a compile error.
 */
export type SurgeApiClient = typeof ApiDefinition extends HttpApi.HttpApi<infer _Id, infer Groups>
  ? HttpApiClient.Client<Groups>
  : never;

export class SurgeApi extends Context.Service<SurgeApi, SurgeApiClient>()("SurgeApi") {
  static layer: Layer.Layer<SurgeApi, never, HttpClient.HttpClient | AuthToken> = Layer.effect(
    SurgeApi,
  )(
    Effect.gen(function*() {
      const baseUrl = yield* Config.nonEmptyString("SURGE_API_URL").pipe(
        Config.withDefault("http://localhost:8100"),
      );

      const auth = yield* AuthToken;

      return yield* HttpApiClient.make(ApiDefinition, {
        baseUrl,
        // One place where every request is decorated, so no call site ever
        // sets an Authorization header and a token expiring mid-session is
        // refreshed underneath the caller.
        //
        // Retries are transport-only — a refused or dropped connection. Not
        // 4xx, which will fail identically however many times it is sent, and
        // not 5xx either: these calls create trips, and a retried booking the
        // server did handle is a second ride. The Idempotency-Key makes that
        // survivable, which is not the same as free.
        transformClient: (client) =>
          client.pipe(
            bearer(auth),
            HttpClient.retryTransient({
              times: 2,
              schedule: Schedule.exponential("100 millis"),
            }),
          ),
      });
    }),
    // A ConfigError means SURGE_API_URL is set to something unusable, and there
    // is no client behaviour that recovers from not knowing where the API is.
  ).pipe(Layer.orDie);
}
