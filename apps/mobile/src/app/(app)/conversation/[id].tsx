import { sessionAtom } from "@/atom/session-atoms.js";
import { ChatComposer } from "@/chat/chat-composer.js";
import { ChatThread } from "@/chat/chat-thread.js";
import { ConversationHeader } from "@/chat/conversation-header.js";
import { useAtomMount, useAtomValue } from "@effect/atom-react";
import { chatPushesAtom } from "@surge/common/atom/chat-atoms";
import { ConversationId } from "@surge/domain/api/Primitives";
import { AsyncResult } from "effect/unstable/reactivity";
import { router, useLocalSearchParams } from "expo-router";
import { View } from "react-native";
import Animated, { useAnimatedKeyboard, useAnimatedStyle } from "react-native-reanimated";
import { useSafeAreaInsets } from "react-native-safe-area-context";

/**
 * One conversation, as a sheet over whatever the person was doing — a trip,
 * the messages list, a notification. Swiping it down puts it away; what was
 * typed is kept for when it opens again.
 *
 * The composer rides the keyboard: the sheet's bottom is the screen's, so the
 * keyboard's height is exactly how far it has to rise.
 */
const ConversationScreen = () => {
  const params = useLocalSearchParams<{ id: string; }>();
  const conversationId = ConversationId.make(params.id);
  const staff = useAtomValue(
    sessionAtom,
    (session) => AsyncResult.isSuccess(session) && session.value.role === "ops",
  );
  useAtomMount(chatPushesAtom);
  const keyboard = useAnimatedKeyboard();
  const insets = useSafeAreaInsets();
  const lifted = useAnimatedStyle(() => ({
    paddingBottom: Math.max(keyboard.height.value, insets.bottom) + 8,
  }));

  return (
    <View className="flex-1 bg-card">
      <ConversationHeader
        conversationId={conversationId}
        staff={staff}
        onClose={() => {
          router.back();
        }}
      />
      <ChatThread conversationId={conversationId} staff={staff} />
      <Animated.View style={lifted}>
        <ChatComposer conversationId={conversationId} staff={staff} />
      </Animated.View>
    </View>
  );
};

export default ConversationScreen;
