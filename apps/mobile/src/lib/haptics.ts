import * as Haptics from "expo-haptics";

/**
 * What the phone gives back under the thumb: a light tap for a press, a
 * selection tick for a choice, a notification for a result — the small
 * physical answers that make a ride app feel sure of itself.
 *
 * Never the only signal: every one accompanies something seen or announced.
 * A device without a Taptic Engine, or a test with the module stubbed, just
 * stays still, so each call swallows its own failure.
 */
const quietly = (feedback: () => Promise<void> | undefined) => {
  void Promise.resolve()
    .then(feedback)
    .catch(() => {});
};

export const haptic = {
  tap: () => {
    quietly(() => Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light));
  },
  select: () => {
    quietly(() => Haptics.selectionAsync());
  },
  success: () => {
    quietly(() => Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success));
  },
  warning: () => {
    quietly(() => Haptics.notificationAsync(Haptics.NotificationFeedbackType.Warning));
  },
} as const;

export type Haptic = keyof typeof haptic;
