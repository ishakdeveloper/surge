import { colors } from "@/lib/theme.js";
import Svg, { Path, Rect } from "react-native-svg";

/**
 * Surge's mark — the web's `SurgeMark` for the phone: a sign-yellow tile with
 * the arrow every wayfinding sign points with. Decorative beside the wordmark,
 * so hidden from assistive tech.
 */
export const SurgeMark = (props: { readonly size?: number; }) => {
  const size = props.size ?? 28;
  return (
    <Svg
      width={size}
      height={size}
      viewBox="0 0 28 28"
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
    >
      <Rect width={28} height={28} rx={8} fill={colors.primary} />
      <Path
        d="M9 19 19 9m0 0h-8.5M19 9v8.5"
        fill="none"
        stroke={colors.foreground}
        strokeWidth={2.5}
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </Svg>
  );
};
