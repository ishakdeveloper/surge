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
jest.mock("react-native-maps", () => {
  const Stub = (props) => props.children ?? null;
  return { __esModule: true, default: Stub, Marker: Stub, Polyline: Stub };
});

jest.mock("react-native-webview", () => ({ WebView: () => null }));

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
