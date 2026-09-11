import { ConversationId, MessageId, TripId, UserId } from "@surge/domain/api/Primitives";
import {
  type Conversation,
  mergeMessages,
  type Message,
  newestSeq,
  readersOf,
  withReceipt,
} from "@surge/domain/chat/Chat";
import { describe, expect, it } from "vitest";

const rider = UserId.make("rider-1");
const driver = UserId.make("drv-1");

const message = (seq: number, sender = rider): Message => ({
  id: MessageId.make(`m-${seq}`),
  conversationId: ConversationId.make("c1"),
  seq,
  senderId: sender,
  senderRole: sender === rider ? "PARTICIPANT_ROLE_RIDER" : "PARTICIPANT_ROLE_DRIVER",
  quickReply: "",
  body: `message ${seq}`,
  clientMessageId: `k-${seq}`,
  createdAt: "2026-09-11T12:00:00Z",
});

const conversation: Conversation = {
  id: ConversationId.make("c1"),
  kind: "CONVERSATION_KIND_TRIP",
  tripId: TripId.make("trip-1"),
  status: "CONVERSATION_STATUS_OPEN",
  subject: "",
  requesterId: rider,
  assigneeId: UserId.make(""),
  lastSeq: 3,
  unread: 0,
  closesAt: "",
  participants: [
    { userId: rider, role: "PARTICIPANT_ROLE_RIDER", lastReadSeq: 3 },
    { userId: driver, role: "PARTICIPANT_ROLE_DRIVER", lastReadSeq: 1 },
  ],
  quickReplies: [],
  createdAt: "2026-09-11T12:00:00Z",
  updatedAt: "2026-09-11T12:00:00Z",
};

describe("messages", () => {
  /**
   * A doorbell can arrive twice, late, or after the catch-up it prompted has
   * already read the message. Whatever order the reads land in, the client
   * holds one copy per seq, oldest first.
   */
  it("merge by seq, whatever order and however often they arrive", () => {
    const held = [message(1), message(2)];
    const merged = mergeMessages(held, [message(4), message(3), message(2)]);
    expect(merged.map((m) => m.seq)).toEqual([1, 2, 3, 4]);
    expect(newestSeq(merged)).toBe(4);
    expect(newestSeq([])).toBe(0);
  });

  it("keep the held array when nothing new arrived", () => {
    const held = [message(1)];
    expect(mergeMessages(held, [])).toBe(held);
  });
});

describe("read markers", () => {
  it("say who, other than the sender, has seen a message", () => {
    expect(readersOf(conversation, message(1))).toEqual([conversation.participants[1]]);
    expect(readersOf(conversation, message(2))).toEqual([]);
    // The sender having read their own message is not "seen".
    expect(readersOf(conversation, message(3, driver))).toEqual([conversation.participants[0]]);
  });

  it("only move forward, and hand back the same conversation when they do not", () => {
    const moved = withReceipt(conversation, driver, 3);
    expect(moved.participants[1]?.lastReadSeq).toBe(3);
    expect(withReceipt(moved, driver, 2)).toBe(moved);
    expect(withReceipt(conversation, UserId.make("nobody"), 9)).toBe(conversation);
  });
});
