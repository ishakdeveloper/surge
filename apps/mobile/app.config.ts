import type { ExpoConfig } from "expo/config";

/**
 * The app's configuration, as code rather than `app.json` for one reason: the
 * Google Maps key an Android build needs comes from the environment and never
 * from the repository. Expo Go carries its own, so development needs none.
 */
// oxlint-disable-next-line effecttsgo/process-env
const googleMapsKey = process.env.GOOGLE_MAPS_ANDROID_API_KEY;

const config: ExpoConfig = {
  name: "Surge",
  slug: "surge",
  scheme: "surge",
  version: "0.0.0",
  orientation: "portrait",
  userInterfaceStyle: "dark",
  backgroundColor: "#0a0a0c",
  ios: {
    bundleIdentifier: "com.ishakdeveloper.surge",
    supportsTablet: false,
  },
  android: {
    package: "com.ishakdeveloper.surge",
  },
  plugins: [
    "expo-router",
    "expo-secure-store",
    [
      "expo-location",
      {
        locationWhenInUsePermission:
          "Surge reports your position to dispatch while you are on shift.",
        locationAlwaysAndWhenInUsePermission:
          "Surge keeps reporting your position to dispatch while you are on shift, with the app in the background.",
        // What `lib/background-location.ts` needs: the `location` background
        // mode on iOS, and a foreground service on Android.
        isIosBackgroundLocationEnabled: true,
        isAndroidBackgroundLocationEnabled: true,
        isAndroidForegroundServiceEnabled: true,
      },
    ],
    ...(googleMapsKey === undefined
      ? []
      : [["react-native-maps", { androidGoogleMapsApiKey: googleMapsKey }] as [string, object]]),
  ],
};

export default config;
