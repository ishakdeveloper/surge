---
version: 1
slug: "apps-web-src-routes-protected-messages"
primary_target: "apps/web/src/routes/_protected/messages"
related_targets: ["apps/web/src/components/chat","apps/web/src/routes/_protected/support"]
---

# Chat

## Scope and mode

Operate. The conversations on web: the rider's trip chat inside the ride
sheet (`apps/web/src/routes/_protected/ride`), the driver's on the drive page,
Messages for riders and drivers (`/messages`, a conversation, contacting
support), and ops' support desk (`/support`). Mobile follows next round on
the same shared atoms (`packages/common/src/atom/chat-atoms.ts`,
`packages/common/src/chat/thread.ts`).

## Audience and job

A rider at a kerb in the rain telling their driver which corner they are on;
a driver between "arrived" and "start" asking where the rider is; someone
with a problem after a trip; an ops agent working a queue at a desk.

## Constraints

- The backend is the chat service on `main`: the socket carries doorbells
  only, messages are read over REST, numbered without gaps, idempotent by
  client key; typing hints; read markers; quick replies per conversation;
  support claim (compare-and-set) and resolve; trip chats close an hour after
  the trip ends.
- Names are roles, never people ("Your driver", "Surge support").
- No toasts. New messages are announced through a `log`; typing is presence
  and is not announced; a failed send is.
- Drafts live in a kept-alive atom per conversation, so leaving loses
  nothing; the contact-support form, which can lose typed text, is guarded.

## Direction contract

Extends the rider world in DESIGN.md; no new tokens.

- Your words in ink bubbles on the right, theirs on grey tiles on the left;
  consecutive bubbles tuck their corners on the sender's side. A time label
  where the conversation paused more than ten minutes.
- Yellow only for the next thing to do: the send button once there is
  something to send, unread counts, the dot on Messages, "New" in the queue.
- Quick replies as the same pills as the popular trips, shown while nothing
  is typed.
- Signature: on the rider's page the conversation takes the sheet's place
  over the live map, the driver still moving above it, with the composer
  pinned in the thumb zone where Go and Cancel sit. Back or Escape returns to
  the trip.
- Pending sends fade until stored; a failed one says why in red with "Try
  again" (same key). Under your newest stored message: Sent, or Seen.
- A closed or resolved conversation reads, with a grey note where the
  composer was.
- The support desk: queue left, conversation right; the open one lifts white
  with an ink ring; "Take it" is the yellow action, "Resolve" quiet; only the
  holder (or anyone, when unclaimed) gets a composer.
