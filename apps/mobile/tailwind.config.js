const { platformSelect } = require("nativewind/theme");

/**
 * The web app's token contract, on Tailwind 3 because that is what NativeWind 4
 * compiles.
 *
 * The names are shadcn's, as in `apps/web/src/app.css`, so a class copied from
 * a web component means the same thing here. The values live in `global.css` as
 * RGB channels — React Native has no `oklch()` — which is what lets opacity
 * modifiers like `bg-destructive/10` work.
 */
const token = (name) => `rgb(var(--${name}) / <alpha-value>)`;

/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ["./src/**/*.{ts,tsx}"],
  presets: [require("nativewind/preset")],
  theme: {
    extend: {
      colors: {
        background: token("background"),
        foreground: token("foreground"),
        card: { DEFAULT: token("card"), foreground: token("card-foreground") },
        primary: { DEFAULT: token("primary"), foreground: token("primary-foreground") },
        secondary: { DEFAULT: token("secondary"), foreground: token("secondary-foreground") },
        muted: { DEFAULT: token("muted"), foreground: token("muted-foreground") },
        accent: { DEFAULT: token("accent"), foreground: token("accent-foreground") },
        destructive: { DEFAULT: token("destructive"), foreground: token("destructive-foreground") },
        success: token("success"),
        border: token("border"),
        input: token("input"),
        ring: token("ring"),
      },
      borderRadius: { sm: "4px", md: "6px", lg: "8px", xl: "10px" },
      fontFamily: {
        mono: platformSelect({ ios: "Menlo", android: "monospace", default: "monospace" }),
      },
    },
  },
};
