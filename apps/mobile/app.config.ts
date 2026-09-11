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
  userInterfaceStyle: "light",
  backgroundColor: "#e9eae6",
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
      "expo-font",
      {
        // Embedded at build time rather than loaded at runtime, so the first
        // frame is already in the face and nothing reflows once it arrives.
        // iOS takes the family name from the files, and picks the weight from
        // `fontWeight`; Android is told the family and which file is which weight.
        ios: {
          fonts: [
            "./assets/fonts/SF-Pro-Rounded-Regular.otf",
            "./assets/fonts/SF-Pro-Rounded-Medium.otf",
            "./assets/fonts/SF-Pro-Rounded-Semibold.otf",
            "./assets/fonts/SF-Pro-Rounded-Bold.otf",
          ],
        },
        android: {
          fonts: [
            {
              fontFamily: "SF Pro Rounded",
              fontDefinitions: [
                { path: "./assets/fonts/SF-Pro-Rounded-Regular.otf", weight: 400 },
                { path: "./assets/fonts/SF-Pro-Rounded-Medium.otf", weight: 500 },
                { path: "./assets/fonts/SF-Pro-Rounded-Semibold.otf", weight: 600 },
                { path: "./assets/fonts/SF-Pro-Rounded-Bold.otf", weight: 700 },
              ],
            },
          ],
        },
      },
    ],
    [
      "expo-location",
      {
        locationWhenInUsePermission:
          "Surge starts your pickup where you are, and reports your position to dispatch while you are on shift.",
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
