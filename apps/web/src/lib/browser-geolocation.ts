import { Geolocation, PositionUnavailable } from "@surge/client/Geolocation";
import type { LatLng } from "@surge/domain/geo/Polyline";
import { Effect, Layer } from "effect";

/**
 * The browser's half of `Geolocation`, provided in `atom/runtime.ts` beside the
 * browser's HTTP client and WebSocket constructor.
 *
 * On a laptop this is usually an IP-derived guess good to a few hundred metres,
 * which is why the driver page also lets a position be placed on the map: for
 * demonstrating the system, choosing where you are beats being told roughly.
 */
export const browserGeolocation = Layer.succeed(Geolocation)({
  current: Effect.callback<LatLng, PositionUnavailable>((resume) => {
    if (typeof navigator === "undefined" || !("geolocation" in navigator)) {
      resume(Effect.fail(new PositionUnavailable({ reason: "Unsupported" })));
      return;
    }

    navigator.geolocation.getCurrentPosition(
      (position) => {
        resume(Effect.succeed({ lat: position.coords.latitude, lng: position.coords.longitude }));
      },
      (error) => {
        const reason = error.code === error.PERMISSION_DENIED
          ? "Denied"
          : error.code === error.TIMEOUT
          ? "Timeout"
          : "Unavailable";
        resume(Effect.fail(new PositionUnavailable({ reason })));
      },
      { enableHighAccuracy: true, timeout: 10_000, maximumAge: 30_000 },
    );
  }),
});
