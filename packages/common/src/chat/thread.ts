import type { UserId } from "@surge/domain/api/Primitives";
import {
  type Conversation,
  type Message,
  type ParticipantRole,
  readersOf,
} from "@surge/domain/chat/Chat";
import { DateTime } from "effect";

/**
 * How a conversation reads, the same on the web and on the phone: what the
 * other side is called, where a thread breaks into runs and time labels, and
 * what a conversation says about itself in a list or instead of a composer.
 *
 * Names are roles, never people. A participant is a user id and a role, and
 * "your driver" is both true and all a rider needs.
 *
 * `staff` is support reading: someone who may not be a participant, for whom
 * the rider is "Rider" rather than "Your rider".
 */

/** Who someone is to the person reading. */
export const roleName = (role: ParticipantRole, staff: boolean): string => {
  switch (role) {
    case "PARTICIPANT_ROLE_RIDER":
      return staff ? "Rider" : "Your rider";
    case "PARTICIPANT_ROLE_DRIVER":
      return staff ? "Driver" : "Your driver";
    case "PARTICIPANT_ROLE_SUPPORT":
      return "Surge support";
    case "PARTICIPANT_ROLE_UNSPECIFIED":
      return "Someone";
  }
};

/** Whoever opened a support conversation, as a role. */
const requesterRole = (conversation: Conversation): ParticipantRole | undefined =>
  conversation.participants.find((participant) => participant.userId === conversation.requesterId)
    ?.role;

/**
 * Who the reader is talking to: the other side of a trip, support for the
 * person who asked, and the person who asked for support.
 */
export const counterpartName = (conversation: Conversation, me: UserId, staff: boolean): string => {
  if (conversation.kind === "CONVERSATION_KIND_SUPPORT") {
    const requester = requesterRole(conversation);
    return staff && requester !== undefined ? roleName(requester, true) : "Surge support";
  }
  if (staff) return "Rider and driver";
  const other = conversation.participants.find((participant) => participant.userId !== me);
  return other === undefined ? "Trip chat" : roleName(other.role, false);
};

/** A conversation's name in a list or a header. A support conversation goes by its subject. */
export const conversationTitle = (conversation: Conversation, me: UserId, staff: boolean): string =>
  conversation.kind === "CONVERSATION_KIND_SUPPORT"
    ? conversation.subject.trim() !== "" ? conversation.subject : "Help from Surge support"
    : counterpartName(conversation, me, staff);

const toDate = (rfc3339: string) => DateTime.toDate(DateTime.makeUnsafe(rfc3339));

const clock = new Intl.DateTimeFormat("nl-NL", { timeStyle: "short" });
const day = new Intl.DateTimeFormat("nl-NL", { dateStyle: "short" });
const dayAndClock = new Intl.DateTimeFormat("nl-NL", {
  weekday: "short",
  day: "numeric",
  month: "short",
  hour: "2-digit",
  minute: "2-digit",
});

/** A time of day, 24-hour, as Amsterdam writes it. */
export const formatClock = (rfc3339: string): string => clock.format(toDate(rfc3339));

/** A moment in a thread or a list: the time alone if it was today, the day too if not. */
export const whenLabel = (rfc3339: string, nowMs: number): string => {
  const date = toDate(rfc3339);
  return day.format(date) === day.format(DateTime.toDate(DateTime.makeUnsafe(nowMs)))
    ? clock.format(date)
    : dayAndClock.format(date);
};

/** What a conversation is doing now, in a list row or under its title. */
export const statusLine = (conversation: Conversation, me: UserId, staff: boolean): string => {
  switch (conversation.status) {
    case "CONVERSATION_STATUS_CLOSED":
      return "Closed";
    case "CONVERSATION_STATUS_RESOLVED":
      return "Resolved";
    case "CONVERSATION_STATUS_OPEN":
    case "CONVERSATION_STATUS_UNSPECIFIED":
      if (conversation.kind === "CONVERSATION_KIND_SUPPORT") {
        if (conversation.assigneeId === "") {
          return staff ? "Waiting for someone to take it" : "Waiting for support to reply";
        }
        if (!staff) return "Support is on it";
        return conversation.assigneeId === me ? "You are handling it" : "Someone else has it";
      }
      return conversation.closesAt !== ""
        ? `Open until ${formatClock(conversation.closesAt)}`
        : "Open while the trip runs";
  }
};

/** What a conversation that takes no more messages says where its composer was. */
export const closedNote = (conversation: Conversation, staff: boolean): string =>
  conversation.status === "CONVERSATION_STATUS_RESOLVED"
    ? staff
      ? "Resolved. It stays here to read, and takes no more messages."
      : "Support resolved this. If you still need help, start a new conversation."
    : "This chat closed after the trip ended. It stays here to read.";

/** What shows under the newest message you sent: whether the other side has looked. */
export const receiptLabel = (conversation: Conversation, message: Message): "Seen" | "Sent" =>
  readersOf(conversation, message).length > 0 ? "Seen" : "Sent";

/** A thread longer than this without a message gets a time label where it picks up again. */
export const PAUSE_MS = 10 * 60 * 1000;

export type ThreadItem =
  | { readonly _tag: "Time"; readonly key: string; readonly at: string; }
  | {
    readonly _tag: "Run";
    readonly key: string;
    readonly mine: boolean;
    /** Whose run it is, for their face and their name. */
    readonly senderId: UserId;
    readonly senderRole: ParticipantRole;
    readonly messages: ReadonlyArray<Message>;
  };

/**
 * A thread as it is drawn: runs of messages by one sender, and a time label
 * at the start and wherever the conversation paused. A pause also starts a new
 * run, so a time never sits in the middle of one.
 */
export const threadItems = (
  messages: ReadonlyArray<Message>,
  me: UserId,
  pauseMs: number = PAUSE_MS,
): ReadonlyArray<ThreadItem> => {
  const items: Array<ThreadItem> = [];
  let run: Array<Message> = [];
  let previousAt: number | undefined;

  const flush = () => {
    const first = run[0];
    if (first === undefined) return;
    items.push({
      _tag: "Run",
      key: `run-${first.seq}`,
      mine: first.senderId === me,
      senderId: first.senderId,
      senderRole: first.senderRole,
      messages: run,
    });
    run = [];
  };

  for (const message of messages) {
    const at = DateTime.toEpochMillis(DateTime.makeUnsafe(message.createdAt));
    const paused = previousAt === undefined || at - previousAt > pauseMs;
    if (paused) {
      flush();
      items.push({ _tag: "Time", key: `time-${message.seq}`, at: message.createdAt });
    } else if (run[0] !== undefined && run[0].senderId !== message.senderId) {
      flush();
    }
    run.push(message);
    previousAt = at;
  }
  flush();
  return items;
};
