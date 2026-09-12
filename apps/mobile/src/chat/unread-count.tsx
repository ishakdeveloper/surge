import { Text } from "@/components/ui/text.js";
import { useAtomMount, useAtomValue } from "@effect/atom-react";
import { chatPushesAtom, conversationsAtom } from "@surge/common/atom/chat-atoms";
import { AsyncResult } from "effect/unstable/reactivity";
import Animated, { ReduceMotion, ZoomIn } from "react-native-reanimated";

/**
 * How many messages are waiting, as a yellow count that pops in: reading them
 * is the next thing to do. Hidden from assistive tech, so the row or tile it
 * sits in says the number in words.
 */
export const UnreadCount = (props: { readonly count: number; }) => (
  <Animated.View
    key={props.count}
    entering={ZoomIn.springify().damping(14).reduceMotion(ReduceMotion.System)}
    accessibilityElementsHidden
    importantForAccessibility="no-hide-descendants"
    className="h-6 min-w-6 items-center justify-center rounded-full bg-primary px-1.5"
  >
    <Text className="text-xs font-semibold text-primary-foreground tabular-nums">
      {props.count > 99 ? "99+" : props.count}
    </Text>
  </Animated.View>
);

/** The same number in words, for the row or tile that shows the count. */
export const unreadWords = (count: number): string =>
  `${count} new ${count === 1 ? "message" : "messages"}`;

/** Everything unread across the caller's conversations, kept live by the socket's doorbells. */
export const useUnreadTotal = (): number => {
  useAtomMount(chatPushesAtom);
  return useAtomValue(
    conversationsAtom,
    (result) =>
      AsyncResult.isSuccess(result)
        ? result.value.reduce((sum, conversation) => sum + conversation.unread, 0)
        : 0,
  );
};
