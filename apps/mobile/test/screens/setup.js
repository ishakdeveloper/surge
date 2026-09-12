/* global jest */
/**
 * Stand-ins for the native modules a screen test cannot load, and for the
 * network. Only true boundaries: the map view, Stripe's SDK, the WebView, the
 * OS background task, the Keychain, and `fetch`. The atoms, the generated API
 * client and the screens are all real.
 *
 * The stubs create no elements themselves. This file goes through the app's
 * Babel config, whose NativeWind JSX runtime rewrites element creation into a
 * helper imported at the top of the file — and Jest refuses a mock factory
 * that reaches outside itself.
 */

/**
 * `fetch` is replaced before any module loads, because better-auth's client
 * captures it when it is created — a spy installed inside a test is too late,
 * and the request would reach whatever is listening on the auth port. By
 * default it refuses, so a test that forgot to stub the auth server fails
 * rather than talking to a real one; tests program it with `jest.mocked(fetch)`.
 */
// Defined rather than assigned: React Native's own setup installs `fetch` as a
// getter-only global, and assigning over one is silently ignored.
Object.defineProperty(globalThis, "fetch", {
  configurable: true,
  writable: true,
  value: jest.fn(() => Promise.reject(new TypeError("fetch is not stubbed in this test"))),
});

jest.mock("@stripe/stripe-react-native", () => require("@stripe/stripe-react-native/jest/mock.js"));

// A map is a native view with nothing to assert on under Jest; its markers and
// route are what the screen computes, and the lists beside it carry the same.
// Mapbox is also the one module here with work at import time — it hands the
// native side an access token — so a stub has to answer that too.
jest.mock("@rnmapbox/maps", () => {
  const Stub = (props) => props.children ?? null;
  return {
    __esModule: true,
    default: Stub,
    MapView: Stub,
    Camera: Stub,
    MarkerView: Stub,
    ShapeSource: Stub,
    SymbolLayer: Stub,
    LineLayer: Stub,
    Images: Stub,
    Image: Stub,
    setAccessToken: async () => null,
    setTelemetryEnabled: () => {},
  };
});

jest.mock("react-native-webview", () => ({ WebView: () => null }));

// Reanimated runs its worklets on a native UI runtime there is none of under
// Jest. Its own mocks settle every animation at once, which is also what a
// test should read: the end state.
jest.mock("react-native-worklets", () => require("react-native-worklets/lib/module/mock"));
jest.mock("react-native-reanimated", () => require("react-native-reanimated/mock"));

// The notch and the home indicator, which a test has neither of: the library's
// own mock answers zero insets without a provider.
jest.mock(
  "react-native-safe-area-context",
  () => require("react-native-safe-area-context/jest/mock").default,
);

// The Taptic Engine: nothing to feel under Jest.
jest.mock("expo-haptics", () => ({
  impactAsync: async () => {},
  selectionAsync: async () => {},
  notificationAsync: async () => {},
  ImpactFeedbackStyle: { Light: "light", Medium: "medium", Heavy: "heavy" },
  NotificationFeedbackType: { Success: "success", Warning: "warning", Error: "error" },
}));

// A face is a native image view; the letter beside it is what a test reads.
jest.mock("expo-image", () => ({ Image: () => null }));

// The photo library and camera, which a screen test never opens.
jest.mock("expo-image-picker", () => ({
  requestCameraPermissionsAsync: async () => ({ granted: false }),
  launchCameraAsync: async () => ({ canceled: true, assets: null }),
  launchImageLibraryAsync: async () => ({ canceled: true, assets: null }),
  CameraType: { front: "front", back: "back" },
}));

jest.mock("expo-image-manipulator", () => ({
  manipulateAsync: async (uri) => ({ uri, width: 512, height: 512, base64: "" }),
  SaveFormat: { JPEG: "jpeg", PNG: "png", WEBP: "webp" },
}));

// The OS's random source. jest-expo's automock answers `undefined`, which is no
// idempotency key at all — a booking would fail to encode rather than be made.
jest.mock("expo-crypto", () => ({ randomUUID: () => require("node:crypto").randomUUID() }));

jest.mock("expo-task-manager", () => ({
  defineTask: jest.fn(),
  isTaskDefined: jest.fn(() => false),
}));

// The Keychain, as better-auth's Expo client uses it: strings in, strings out,
// both sync and async. Empty at the start of every test file.
jest.mock("expo-secure-store", () => {
  const store = new Map();
  return {
    getItem: (key) => store.get(key) ?? null,
    setItem: (key, value) => {
      store.set(key, value);
    },
    getItemAsync: async (key) => store.get(key) ?? null,
    setItemAsync: async (key, value) => {
      store.set(key, value);
    },
    deleteItemAsync: async (key) => {
      store.delete(key);
    },
  };
});

// Deep links, as better-auth's Expo client uses them: it builds its
// `expo-origin` header from the app's scheme, which under Expo comes from the
// manifest — and there is no manifest under Jest.
jest.mock("expo-linking", () => ({
  createURL: (path = "") => `surge://${String(path).replace(/^\//, "")}`,
  parse: (url) => ({
    scheme: "surge",
    hostname: null,
    path: url.replace(/^surge:\/\//, ""),
    queryParams: {},
  }),
  addEventListener: () => ({ remove: () => {} }),
  getInitialURL: async () => null,
}));
