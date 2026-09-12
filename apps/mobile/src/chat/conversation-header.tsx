import { sessionAtom } from "@/atom/session-atoms.js";
import { ActionSheet } from "@/components/app/action-sheet.js";
import { SheetButton, SheetHeader } from "@/components/app/sheet-header.js";
import { PersonAvatar, usePersonName } from "@/components/profile/avatar.js";
import { colors } from "@/lib/theme.js";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import Ionicons from "@expo/vector-icons/Ionicons";
import { conversationAtom, resolveConversation, typingAtom } from "@surge/common/atom/chat-atoms";
import { conversationTitle, roleName, statusLine } from "@surge/common/chat/thread";
import type { ConversationId, UserId } from "@surge/domain/api/Primitives";
import { type Conversation, isOpen, type Participant } from "@surge/domain/chat/Chat";
import { AsyncResult } from "effect/unstable/reactivity";
import * as React from "react";
import { View } from "react-native";

/**
 * Whose face the header shows: the other side of a trip, and for support the
 * person who asked. Support itself has no one face, so the person asking sees
 * the lifebuoy instead.
 */
const counterpartOf = (
  conversation: Conversation,
  me: UserId,
  staff: boolean,
): Participant | undefined =>
  conversation.kind === "CONVERSATION_KIND_SUPPORT"
    ? staff
      ? conversation.participants.find((participant) =>
        participant.userId === conversation.requesterId
      )
      : undefined
    : staff
    ? undefined
    : conversation.participants.find((participant) => participant.userId !== me);

const PersonName = (props: { readonly userId: UserId; readonly role: string; }) => (
  <>{usePersonName(props.userId, props.role)}</>
);

/**
 * A conversation sheet's header — the web's `ConversationHeader`: who it is
 * with and what it is doing now, "Typing…" while the other side is. Someone
 * who asked support for help can mark it solved from here.
 */
export const ConversationHeader = (props: {
  readonly conversationId: ConversationId;
  readonly staff: boolean;
  readonly onClose: () => void;
}) => {
  const view = useAtomValue(conversationAtom(props.conversationId));
  const typing = useAtomValue(typingAtom(props.conversationId));
  const me = useAtomValue(
    sessionAtom,
    (session) => AsyncResult.isSuccess(session) ? session.value.userId : undefined,
  );
  const resolve = useAtomSet(resolveConversation);
  const [asking, setAsking] = React.useState(false);

  const conversation = AsyncResult.isSuccess(view) ? view.value.conversation : undefined;
  const othersTyping = AsyncResult.isSuccess(typing) && AsyncResult.isSuccess(view)
    && typing.value.some((userId) => userId !== view.value.me);
  const other = conversation === undefined || me === undefined
    ? undefined
    : counterpartOf(conversation, me, props.staff);
  const support = conversation?.kind === "CONVERSATION_KIND_SUPPORT";
  const resolvable = conversation !== undefined && support && isOpen(conversation)
    && (props.staff || conversation.requesterId === me);

  return (
    <>
      <SheetHeader
        onClose={props.onClose}
        leading={other !== undefined
          ? <PersonAvatar userId={other.userId} size="md" />
          : support && !props.staff
          ? (
            <View
              accessibilityElementsHidden
              importantForAccessibility="no-hide-descendants"
              className="size-11 items-center justify-center rounded-full bg-tile"
            >
              <Ionicons name="help-buoy" size={22} color={colors.foreground} />
            </View>
          )
          : undefined}
        title={conversation === undefined || me === undefined
          ? "Conversation"
          : other !== undefined && conversation.kind === "CONVERSATION_KIND_TRIP"
          ? <PersonName userId={other.userId} role={roleName(other.role, props.staff)} />
          : conversationTitle(conversation, me, props.staff)}
        subtitle={othersTyping
          ? "Typing…"
          : conversation === undefined || me === undefined
          ? " "
          : statusLine(conversation, me, props.staff)}
        trailing={resolvable
          ? (
            <SheetButton
              icon="ellipsis-horizontal"
              label="More"
              onPress={() => {
                setAsking(true);
              }}
            />
          )
          : undefined}
      />
      {conversation !== undefined && (
        <ActionSheet
          visible={asking}
          title={props.staff ? "Resolve this conversation?" : "Is it sorted?"}
          message={props.staff
            ? "The person who asked can open a new one if they need to."
            : "Marking it solved closes this conversation. You can always contact support again."}
          onClose={() => {
            setAsking(false);
          }}
          options={[{
            label: props.staff ? "Resolve" : "Mark as solved",
            icon: "checkmark-circle-outline",
            onPress: () => {
              resolve(conversation.id);
            },
          }]}
        />
      )}
    </>
  );
};
