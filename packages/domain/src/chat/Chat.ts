import { Array as Arr, Order } from "effect";
import type { UserId } from "../api/Primitives.js";
import {
  ChatGet200,
  type ChatListMessages200,
  type ChatServiceSendMessageBody,
} from "../api/SurgeApi.js";

/**
 * Names for the chat shapes the generated client returns, and the rules about
 * them no document can state — the arrangement `../trip/Trip.ts` makes, for
 * its reason: a field that moves in `proto/chat.proto` moves here on the next
 * `make proto`, with nothing to keep in step.
 */

/** A conversation, as the caller sees it: their unread count, and what they can send now. */
export type Conversation = ChatGet200["conversation"];

/** A message. Numbered within its conversation from 1, without gaps. */
export type Message = ChatListMessages200["messages"][number];

/** Someone in a conversation, and how far they have read. */
export type Participant = Conversation["participants"][number];

/** A message sent with one tap: its code, and the English text it becomes. */
export type QuickReply = Conversation["quickReplies"][number];

export type ParticipantRole = Participant["role"];

/**
 * A message as it is sent: text or a quick reply's code, never both, and the
 * key that makes a retry the same message. The caller makes the key and keeps
 * it across retries, as with booking a trip.
 */
export type Draft = ChatServiceSendMessageBody;

/**
 * Whether a conversation takes messages. The protobuf enum names, verbatim, as
 * a runtime schema lifted out of the generated client — the choice
 * `TripStatus` makes.
 */
export const ConversationStatus = ChatGet200.fields.conversation.fields.status;
export type ConversationStatus = typeof ConversationStatus.Type;

/** Who a conversation is between: a trip's rider and driver, or someone and support. */
export const ConversationKind = ChatGet200.fields.conversation.fields.kind;
export type ConversationKind = typeof ConversationKind.Type;

/**
 * Whether it takes messages. A closed trip conversation and a resolved support
 * one both still read; neither writes.
 */
export const isOpen = (conversation: Conversation): boolean =>
  conversation.status === "CONVERSATION_STATUS_OPEN";

/** The newest seq held, or 0 for none — what a client asks for everything after. */
export const newestSeq = (messages: ReadonlyArray<Message>): number => messages.at(-1)?.seq ?? 0;

const bySeq = Order.mapInput(Order.Number, (message: Message) => message.seq);

/**
 * Messages held, with more read. One per seq, oldest first; a message read
 * again replaces the copy held. The same messages given twice change nothing,
 * so a doorbell that arrives twice is harmless.
 */
export const mergeMessages = (
  held: ReadonlyArray<Message>,
  incoming: ReadonlyArray<Message>,
): ReadonlyArray<Message> => {
  if (incoming.length === 0) return held;
  const merged = new Map([...held, ...incoming].map((message) => [message.seq, message]));
  return Arr.sort(merged.values(), bySeq);
};

/**
 * Everyone but the sender who has read up to this message: what "Seen" under
 * it means. Empty until the other side has looked.
 */
export const readersOf = (
  conversation: Conversation,
  message: Message,
): ReadonlyArray<Participant> =>
  conversation.participants.filter((participant) =>
    participant.userId !== message.senderId && participant.lastReadSeq >= message.seq
  );

/**
 * A participant's read marker moved to `seq`.
 *
 * Never backwards, as the server's never does, so a receipt that arrives late
 * is not news. Nothing moving returns the conversation it was given, so a view
 * derived from it does not re-render for nothing.
 */
export const withReceipt = (
  conversation: Conversation,
  userId: UserId,
  seq: number,
): Conversation =>
  conversation.participants.some((participant) =>
      participant.userId === userId && participant.lastReadSeq < seq
    )
    ? {
      ...conversation,
      participants: conversation.participants.map((participant) =>
        participant.userId === userId ? { ...participant, lastReadSeq: seq } : participant
      ),
    }
    : conversation;
