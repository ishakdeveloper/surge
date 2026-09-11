/**
 * The palette as plain colours, for the few places that take a colour rather
 * than a class: navigation chrome, the status bar, placeholder text, spinners.
 *
 * The same values as the channels in `global.css`, written as hex. Everything
 * that can take a `className` should, so this stays short.
 */
export const colors = {
  background: "#0a0a0c",
  foreground: "#eeeeee",
  card: "#121215",
  border: "#212124",
  primary: "#3a93e6",
  mutedForeground: "#717174",
} as const;
