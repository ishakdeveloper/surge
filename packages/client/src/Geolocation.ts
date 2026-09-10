import type { LatLng } from "@surge/domain/geo/Polyline";
import { Context, type Effect, Schema } from "effect";

/**
 * Where the device is, as the driver app needs it.
 *
 * An interface with no implementation here, on purpose — the same arrangement
 * as `HttpClient` and `Socket`. Every platform finds its position differently:
 * the browser through `navigator.geolocation`, React Native through a native
 * module with background permissions the browser has no equivalent of. So this
 * package declares what it needs and each app provides it at its edge, and the
 * Expo driver app gets real GPS by supplying one layer rather than by editing a
 * caller.
 */
export class PositionUnavailable
  extends Schema.TaggedError<PositionUnavailable>()("PositionUnavailable", {
    reason: Schema.Literals(["Unsupported", "Denied", "Unavailable", "Timeout"]),
  })
{}

export interface GeolocationService {
  /** One fix, now. */
  readonly current: Effect.Effect<LatLng, PositionUnavailable>;
}

export class Geolocation
  extends Context.Service<Geolocation, GeolocationService>()("Geolocation")
{}
