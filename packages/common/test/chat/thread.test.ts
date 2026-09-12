import {
  closedNote,
  conversationTitle,
  counterpartName,
  receiptLabel,
  roleName,
  statusLine,
  threadItems,
} from "@surge/common/chat/thread";
import { ConversationId, MessageId, TripId, UserId } from "@surge/domain/api/Primitives";
import type { Conversation, Message } from "@surge/domain/chat/Chat";
import { describe, expect, it } from "vitest";

const rider = UserId.make("rider-1");
const driver = UserId.make("driver-1");
const agent = UserId.make("ops-1");

const message = (seq: number, sender: UserId, at: string): Message => ({
  id: MessageId.make(`m${seq}`),
  conversationId: ConversationId.make("c1"),
  seq,
  senderId: sender,
  senderRole: sender === rider ? "PARTICIPANT_ROLE_RIDER" : "PARTICIPANT_ROLE_DRIVER",
  quickReply: "",
  body: `message ${seq}`,
  clientMessageId: `key-${seq}`,
  createdAt: at,
});

const tripChat = (overrides: Partial<Conversation> = {}): Conversation => ({
  id: ConversationId.make("c1"),
  kind: "CONVERSATION_KIND_TRIP",
  tripId: TripId.make("trip-1"),
  status: "CONVERSATION_STATUS_OPEN",
  subject: "",
  requesterId: rider,
  assigneeId: UserId.make(""),
  lastSeq: 0,
  unread: 0,
  closesAt: "",
  participants: [
    { userId: rider, role: "PARTICIPANT_ROLE_RIDER", lastReadSeq: 0 },
    { userId: driver, role: "PARTICIPANT_ROLE_DRIVER", lastReadSeq: 0 },
  ],
  quickReplies: [],
  createdAt: "2026-09-11T18:00:00Z",
  updatedAt: "2026-09-11T18:00:00Z",
  ...overrides,
});

const supportChat = (overrides: Partial<Conversation> = {}): Conversation =>
  tripChat({
    kind: "CONVERSATION_KIND_SUPPORT",
    subject: "Charged twice",
    participants: [{ userId: rider, role: "PARTICIPANT_ROLE_RIDER", lastReadSeq: 0 }],
    ...overrides,
  });

describe("names", () => {
  it("names the other side by role, and plainly for support", () => {
    expect(roleName("PARTICIPANT_ROLE_DRIVER", false)).toBe("Your driver");
    expect(roleName("PARTICIPANT_ROLE_DRIVER", true)).toBe("Driver");
    expect(roleName("PARTICIPANT_ROLE_SUPPORT", false)).toBe("Surge support");
  });

  it("calls a trip chat by whoever is on the other side of it", () => {
    expect(conversationTitle(tripChat(), rider, false)).toBe("Your driver");
    expect(conversationTitle(tripChat(), driver, false)).toBe("Your rider");
    expect(conversationTitle(tripChat(), agent, true)).toBe("Rider and driver");
  });

  it("calls a support conversation by its subject, and its requester by role for support", () => {
    expect(conversationTitle(supportChat(), rider, false)).toBe("Charged twice");
    expect(conversationTitle(supportChat({ subject: " " }), rider, false))
      .toBe("Help from Surge support");
    expect(counterpartName(supportChat(), rider, false)).toBe("Surge support");
    expect(counterpartName(supportChat(), agent, true)).toBe("Rider");
  });
});

describe("threadItems", () => {
  it("runs messages by sender, with a time label at the start", () => {
    const items = threadItems([
      message(1, rider, "2026-09-11T18:00:00Z"),
      message(2, rider, "2026-09-11T18:00:20Z"),
      message(3, driver, "2026-09-11T18:01:00Z"),
    ], rider);

    expect(items.map((item) => item._tag)).toEqual(["Time", "Run", "Run"]);
    const [, mine, theirs] = items;
    expect(mine).toMatchObject({ _tag: "Run", mine: true });
    expect(mine?._tag === "Run" && mine.messages.map((m) => m.seq)).toEqual([1, 2]);
    expect(theirs).toMatchObject({
      _tag: "Run",
      mine: false,
      senderRole: "PARTICIPANT_ROLE_DRIVER",
    });
  });

  it("labels the time where the conversation picks up after a pause, and starts a new run", () => {
    const items = threadItems([
      message(1, rider, "2026-09-11T18:00:00Z"),
      message(2, rider, "2026-09-11T18:30:00Z"),
    ], rider);

    expect(items.map((item) => item._tag)).toEqual(["Time", "Run", "Time", "Run"]);
  });

  it("has nothing to draw for no messages", () => {
    expect(threadItems([], rider)).toEqual([]);
  });
});

describe("status", () => {
  it("says who has a support conversation, to support and to the person who asked", () => {
    expect(statusLine(supportChat(), rider, false)).toBe("Waiting for support to reply");
    expect(statusLine(supportChat(), agent, true)).toBe("Waiting for someone to take it");
    expect(statusLine(supportChat({ assigneeId: agent }), agent, true)).toBe("You are handling it");
    expect(statusLine(supportChat({ assigneeId: UserId.make("ops-2") }), agent, true))
      .toBe("Someone else has it");
    expect(statusLine(supportChat({ assigneeId: agent }), rider, false)).toBe("Support is on it");
  });

  it("says when a trip chat stops taking messages, once the trip has ended", () => {
    expect(statusLine(tripChat(), rider, false)).toBe("Open while the trip runs");
    expect(statusLine(tripChat({ closesAt: "2026-09-11T19:41:00Z" }), rider, false))
      .toMatch(/^Open until /);
    expect(statusLine(tripChat({ status: "CONVERSATION_STATUS_CLOSED" }), rider, false))
      .toBe("Closed");
  });

  it("tells the person who asked where to go once support has resolved it", () => {
    const resolved = supportChat({ status: "CONVERSATION_STATUS_RESOLVED" });
    expect(closedNote(resolved, false)).toMatch(/start a new conversation/);
    expect(closedNote(tripChat({ status: "CONVERSATION_STATUS_CLOSED" }), rider !== driver))
      .toMatch(/closed after the trip ended/);
  });

  it("marks your newest message seen once the other side has read up to it", () => {
    const sent = message(3, rider, "2026-09-11T18:00:00Z");
    expect(receiptLabel(tripChat(), sent)).toBe("Sent");
    const read = tripChat({
      participants: [
        { userId: rider, role: "PARTICIPANT_ROLE_RIDER", lastReadSeq: 3 },
        { userId: driver, role: "PARTICIPANT_ROLE_DRIVER", lastReadSeq: 3 },
      ],
    });
    expect(receiptLabel(read, sent)).toBe("Seen");
  });
});
