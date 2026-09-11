import { Geolocation, PositionUnavailable } from "@surge/client/Geolocation";
import type { LatLng } from "@surge/domain/geo/Polyline";
import { Cause, Effect, Layer, Queue, Stream } from "effect";

/**
 * The browser's half of `Geolocation`, provided in `atom/platform.ts` beside the
 * browser's HTTP client and WebSocket constructor.
 *
 * On a laptop this is usually an IP-derived guess good to a few hundred metres,
 * which is why the driver page also lets a position be placed on the map: for
 * demonstrating the system, choosing where you are beats being told roughly.
 */

const supported = () => typeof navigator !== "undefined" && "geolocation" in navigator;

const refusal = (error: GeolocationPositionError) =>
  new PositionUnavailable({
    reason: error.code === error.PERMISSION_DENIED
      ? "Denied"
      : error.code === error.TIMEOUT
      ? "Timeout"
      : "Unavailable",
  });

const toLatLng = (position: GeolocationPosition): LatLng => ({
  lat: position.coords.latitude,
  lng: position.coords.longitude,
});

export const browserGeolocation = Layer.succeed(Geolocation)({
  current: Effect.callback<LatLng, PositionUnavailable>((resume) => {
    if (!supported()) {
      resume(Effect.fail(new PositionUnavailable({ reason: "Unsupported" })));
      return;
    }

    navigator.geolocation.getCurrentPosition(
      (position) => {
        resume(Effect.succeed(toLatLng(position)));
      },
      (error) => {
        resume(Effect.fail(refusal(error)));
      },
      { enableHighAccuracy: true, timeout: 10_000, maximumAge: 30_000 },
    );
  }),

  /** `watchPosition`, as a stream. A tab in the background is throttled by the browser; nothing here can change that. */
  watch: Stream.callback<LatLng, PositionUnavailable>((queue) =>
    Effect.gen(function*() {
      if (!supported()) return yield* new PositionUnavailable({ reason: "Unsupported" });

      const id = navigator.geolocation.watchPosition(
        (position) => {
          Queue.offerUnsafe(queue, toLatLng(position));
        },
        (error) => {
          Queue.failCauseUnsafe(queue, Cause.fail(refusal(error)));
        },
        { enableHighAccuracy: true, maximumAge: 5_000 },
      );
      yield* Effect.addFinalizer(() =>
        Effect.sync(() => {
          navigator.geolocation.clearWatch(id);
        })
      );
    })
  ),
});
