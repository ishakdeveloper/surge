/**
 * NativeWind compiles `className` into React Native styles at build time. It
 * needs both halves: the JSX runtime swap, so every host component accepts
 * `className`, and its own preset, which does the compiling.
 */
module.exports = function(api) {
  api.cache(true);
  return {
    presets: [["babel-preset-expo", { jsxImportSource: "nativewind" }], "nativewind/babel"],
  };
};
