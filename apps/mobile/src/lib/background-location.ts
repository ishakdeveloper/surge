import * as Location from "expo-location";
import * as TaskManager from "expo-task-manager";

/**
 * Location while the app is not in front.
 *
 * iOS suspends an app's JavaScript when it leaves the screen, so a driver who
 * pocketed the phone used to vanish from the matcher's map. The OS will keep
 * delivering fixes to a registered background task instead — which is the only
 * way to get them — and this module is that task and the hand-off from it to
 * `native-geolocation.ts`, whose `watch` stream the ping loop reads.
 *
 * `defineTask` must run in the bundle's global scope, before anything mounts,
 * because the OS may start the app headless just to run it. So the root layout
 * imports this module first, for its side effect.
 *
 * Needs a development build. Expo Go cannot register background location on
 * iOS; there `startBackgroundUpdates` rejects and `native-geolocation.ts` falls
 * back to foreground updates, which is still live tracking while the app is
 * open.
 */
export const BACKGROUND_LOCATION_TASK = "surge-driver-location";

type Listener = (location: Location.LocationObject) => void;

const listeners = new Set<Listener>();

TaskManager.defineTask<{ readonly locations: ReadonlyArray<Location.LocationObject>; }>(
  BACKGROUND_LOCATION_TASK,
  (body) => {
    // A headless launch has no listeners, and a fix nobody can send is
    // dropped: it would be stale by the time a socket was open to carry it.
    if (body.error === null) {
      for (const location of body.data.locations) {
        for (const listener of listeners) listener(location);
      }
    }
    return Promise.resolve();
  },
);

/** Fixes from the background task, for as long as the returned function is not called. */
export const subscribeBackgroundLocations = (listener: Listener): () => void => {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
};

/**
 * Every four seconds or ten metres, like the web's ping loop — more would be
 * battery spent on positions the matcher does not need.
 */
export const startBackgroundUpdates = () =>
  Location.startLocationUpdatesAsync(BACKGROUND_LOCATION_TASK, {
    accuracy: Location.Accuracy.High,
    timeInterval: 4_000,
    distanceInterval: 10,
    activityType: Location.LocationActivityType.AutomotiveNavigation,
    pausesUpdatesAutomatically: false,
    // The blue pill on iOS, and Android's required notification: a driver
    // should always be able to see that they are being tracked.
    showsBackgroundLocationIndicator: true,
    foregroundService: {
      notificationTitle: "Surge is using your location",
      notificationBody: "Reporting your position to dispatch while you are on shift.",
    },
  });

export const stopBackgroundUpdates = async () => {
  if (await Location.hasStartedLocationUpdatesAsync(BACKGROUND_LOCATION_TASK)) {
    await Location.stopLocationUpdatesAsync(BACKGROUND_LOCATION_TASK);
  }
};
