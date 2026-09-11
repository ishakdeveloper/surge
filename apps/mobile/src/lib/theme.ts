/**
 * The palette as plain colours, for the few places that take a colour rather
 * than a class: navigation chrome, the status bar, placeholder text, spinners,
 * and the map's route and markers.
 *
 * The same values as the channels in `global.css`, written as hex. Everything
 * that can take a `className` should, so this stays short.
 */
export const colors = {
  background: "#f7f7f5",
  foreground: "#1a1a1a",
  card: "#ffffff",
  tile: "#f0f0ed",
  border: "#e3e3df",
  primary: "#ffd200",
  secondary: "#1a1a1a",
  success: "#067a3e",
  mutedForeground: "#5f636a",
} as const;
