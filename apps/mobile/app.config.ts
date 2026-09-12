import type { ExpoConfig } from "expo/config";

/**
 * The app's configuration, as code rather than `app.json` for one reason: the
 * Mapbox token a build needs to fetch the native SDK comes from the
 * environment and never from the repository.
 *
 * Mapbox's native SDK is fetched at build time with a secret token.
 */
// oxlint-disable-next-line effecttsgo/process-env
const mapboxDownloadToken = process.env.MAPBOX_DOWNLOADS_TOKEN;

const config: ExpoConfig = {
  name: "Surge",
  slug: "surge",
  scheme: "surge",
  version: "0.0.0",
  // The EAS project this builds as, and the account that owns it. `eas init`
  // writes these into a static app.json itself; a config that is code has to
  // be told.
  owner: "isakdev",
  extra: { eas: { projectId: "d561e151-a6bd-485b-9d26-ee7208e710fa" } },
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
    [
      "@rnmapbox/maps",
      {
        // Fetching Mapbox's native SDK needs a secret token with
        // DOWNLOADS:READ. It is read at build time — from `.env` locally, from
        // the EAS secret of the same name in the cloud — and never ends up in
        // the bundle. The public token the app ships with is
        // EXPO_PUBLIC_MAPBOX_TOKEN.
        RNMapboxMapsDownloadToken: mapboxDownloadToken,
      },
    ],
  ],
};

export default config;
