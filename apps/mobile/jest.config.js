const path = require("node:path");

const workspaceRoot = path.resolve(__dirname, "../..");

/**
 * Packages Jest must run through Babel rather than load as they are: React
 * Native and Expo ship untranspiled source, and Effect, better-auth and their
 * dependencies ship ES modules only. Everything else in `node_modules` is left
 * alone.
 *
 * pnpm keeps packages at `node_modules/.pnpm/<name>@<version>/node_modules/<name>`,
 * so the pattern allows an optional store segment before the name.
 */
const TRANSFORM = [
  "(jest-)?react-native",
  "@react-native(-community)?",
  "expo(nent)?",
  "@expo(nent)?/.*",
  "expo-.*",
  "react-navigation",
  "@react-navigation/.*",
  // expo-router's, which ships ES modules only.
  "standard-navigation",
  "nativewind",
  "react-native-css-interop",
  "@stripe/stripe-react-native",
  "react-native-.*",
  "effect",
  "@effect/.*",
  // Effect's own dependencies, which ship ES modules only.
  "msgpackr",
  "fast-check",
  "pure-rand",
  "@standard-schema/.*",
  "better-auth",
  "@better-auth/.*",
  "@better-fetch/.*",
  "better-call",
  // better-auth's, likewise.
  "defu",
  "jose",
  "kysely",
  "@noble/.*",
  "rou3",
  "set-cookie-parser",
  "nanostores",
  "@nanostores/.*",
  "@lucas-barake/.*",
  "class-variance-authority",
  "tailwind-merge",
].join("|");

/** jest-expo's preset, spread rather than named, so it can be extended rather than replaced. */
const expo = require("jest-expo/jest-preset");

/**
 * The preset's Babel transformer, told it is compiling for the engine Metro
 * compiles the app for. Metro says `hermes`, and babel-preset-expo answers with
 * its Hermes V1 profile; jest-expo says nothing, and gets the legacy profile,
 * whose loose object spread is an `Object.assign` — which calls a getter
 * instead of copying it. Effect's `Context` prototype is built that way, so
 * every context read `mapUnsafe` as `undefined` and no layer could build.
 */
const [babelJest, babelOptions] = expo.transform["\\.[jt]sx?$"];
const asMetro = [babelJest, {
  ...babelOptions,
  caller: { ...babelOptions.caller, engine: "hermes" },
}];

/**
 * The screens, rendered under Jest over the production atoms and a fake
 * platform (`@surge/common/testing/fake-platform`).
 *
 * Jest rather than Vitest because React Native's code only runs through its
 * own Babel and module setup, which `jest-expo` provides — the pure logic
 * stays on Vitest in `test/`. Resolution mirrors `metro.config.js`: `@/` is
 * `src`, the workspace packages are their source, and a `.js` import finds the
 * `.ts` beside it.
 */
module.exports = {
  ...expo,
  // better-auth ships `.mjs`, which the preset's pattern does not match, and an
  // allowlisted file no transform matches is loaded as it is.
  transform: { ...expo.transform, "\\.[jt]sx?$": asMetro, "\\.mjs$": asMetro },
  testMatch: ["<rootDir>/test/screens/**/*.test.tsx"],
  // Added to the preset's, which define `__DEV__` and React Native's own mocks.
  setupFiles: [...expo.setupFiles, "<rootDir>/test/screens/setup.js"],
  // Ours first: Jest tries mappings in order, and the preset's own `^@/(.*)$`
  // keeps the `.js` a TypeScript import is written with.
  moduleNameMapper: {
    // One React: the app's own. pnpm gives NativeWind no React of its own, so
    // lookup from it climbs to the workspace's hoisted copy — the web app's —
    // and components built through its JSX runtime call hooks on the wrong one.
    "^react$": require.resolve("react"),
    "^react/(.*)$": `${path.dirname(require.resolve("react/package.json"))}/$1`,
    "^@/(.*?)(\\.js)?$": "<rootDir>/src/$1",
    "^@surge/(client|common|domain)/(.*?)(\\.js)?$": `${workspaceRoot}/packages/$1/src/$2`,
    "^(\\.{1,2}/.*)\\.js$": "$1",
    ...expo.moduleNameMapper,
  },
  transformIgnorePatterns: [
    `node_modules/(?!(?:\\.pnpm/[^/]+/node_modules/)?(${TRANSFORM})(/|$))`,
  ],
};
