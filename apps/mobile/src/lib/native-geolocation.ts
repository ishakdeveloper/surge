import {
  startBackgroundUpdates,
  stopBackgroundUpdates,
  subscribeBackgroundLocations,
} from "@/lib/background-location.js";
import { Geolocation, PositionUnavailable } from "@surge/client/Geolocation";
import type { LatLng } from "@surge/domain/geo/Polyline";
import { Effect, Layer, Queue, Stream } from "effect";
import * as Location from "expo-location";

/**
 * The phone's half of `Geolocation`, provided in `atom/platform.ts` beside the
 * fetch client and WebSocket constructor — the arrangement
 * `packages/client/src/Geolocation.ts` describes, and why that file has no
 * implementation of its own.
 *
 * Permission is asked when a driver chooses to follow their GPS, not at
 * launch: asked out of context, people say no.
 */
const unavailable = new PositionUnavailable({ reason: "Unavailable" });

const toLatLng = (location: Location.LocationObject): LatLng => ({
  lat: location.coords.latitude,
  lng: location.coords.longitude,
});

const foregroundPermission = Effect.tryPromise({
  try: () => Location.requestForegroundPermissionsAsync(),
  catch: () => unavailable,
}).pipe(
  Effect.flatMap((permission) =>
    permission.granted ? Effect.void : Effect.fail(new PositionUnavailable({ reason: "Denied" }))
  ),
);

/**
 * Background permission is a second question, and "no" to it is an answer
 * rather than a failure: the driver is still followed, just only while the app
 * is open.
 */
const backgroundGranted = Effect.tryPromise(() => Location.requestBackgroundPermissionsAsync())
  .pipe(
    Effect.map((permission) => permission.granted),
    Effect.orElseSucceed(() => false),
  );

export const nativeGeolocation = Layer.succeed(Geolocation)({
  current: Effect.gen(function*() {
    yield* foregroundPermission;

    const fix = yield* Effect.tryPromise({
      try: () => Location.getCurrentPositionAsync({ accuracy: Location.Accuracy.High }),
      // Location services switched off, or no fix to be had indoors.
      catch: () => unavailable,
    }).pipe(
      // The browser's `timeout` option, which expo-location does not take.
      Effect.timeoutOrElse({
        duration: "10 seconds",
        orElse: () => Effect.fail(new PositionUnavailable({ reason: "Timeout" })),
      }),
    );

    return toLatLng(fix);
  }),

  /**
   * From the background task when the OS allows one, so a driver with the
   * phone in a pocket stays on the map; otherwise from foreground updates.
   * Either way at most every four seconds or ten metres, like the ping loop.
   */
  watch: Stream.callback<LatLng, PositionUnavailable>((queue) =>
    Effect.gen(function*() {
      yield* foregroundPermission;
      const emit = (location: Location.LocationObject) => {
        Queue.offerUnsafe(queue, toLatLng(location));
      };

      if (yield* backgroundGranted) {
        // Rejects under Expo Go on iOS, which cannot register background
        // location — the fallback below is the answer to that, not an error.
        const started = yield* Effect.tryPromise(() => startBackgroundUpdates()).pipe(
          Effect.as(true),
          Effect.orElseSucceed(() => false),
        );
        if (started) {
          const unsubscribe = subscribeBackgroundLocations(emit);
          yield* Effect.addFinalizer(() =>
            Effect.sync(unsubscribe).pipe(
              Effect.andThen(Effect.tryPromise(() => stopBackgroundUpdates())),
              Effect.ignore,
            )
          );
          return;
        }
      }

      const subscription = yield* Effect.tryPromise({
        try: () =>
          Location.watchPositionAsync(
            { accuracy: Location.Accuracy.High, timeInterval: 4_000, distanceInterval: 10 },
            emit,
          ),
        catch: () => unavailable,
      });
      yield* Effect.addFinalizer(() =>
        Effect.sync(() => {
          subscription.remove();
        })
      );
    })
  ),
});
