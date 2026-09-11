import { cn } from "@/lib/utils.js";
import * as React from "react";
import { AccessibilityInfo, Platform, View } from "react-native";

/**
 * A status that is announced when it changes — the web's
 * `<output aria-live="polite">`.
 *
 * Android has live regions and reads `accessibilityLiveRegion`. iOS has no
 * equivalent, so the new message is announced explicitly. Not on first render:
 * arriving on a screen is announced by the navigator, and saying the status
 * again on top of it is noise.
 *
 * Only for the updates a person is waiting on — a trip moving, an offer
 * arriving. Announcing everything is how people learn to ignore announcements.
 */
export const Announced = (props: {
  readonly message: string;
  readonly className: string;
  readonly children: React.ReactNode;
}) => {
  const first = React.useRef(true);

  React.useEffect(() => {
    if (first.current) {
      first.current = false;
      return;
    }
    if (Platform.OS === "ios") AccessibilityInfo.announceForAccessibility(props.message);
  }, [props.message]);

  return (
    <View accessibilityLiveRegion="polite" className={cn(props.className)}>
      {props.children}
    </View>
  );
};
