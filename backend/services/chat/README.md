# chat

The conversations riders and drivers have: with each other about a trip, and
with support.

## Why it is separate

For the reason payments is. It holds the push provider's credential and is
the only process that calls it, and a slow Expo must never stall the trip
state machine. Trip never calls chat. Trip publishes facts on
`trip.lifecycle`, chat opens and closes conversations from them, and chat asks
trip over gRPC only whose trip something is. It asks as the caller, so trip's
own rule decides: the rider, the assigned driver, or ops.

## How a message travels

```
client ── POST /v1/conversations/{id}/messages ──▶ gateway ──gRPC──▶ chat
                                                                     │  one transaction:
                                                                     │  seq = last_seq + 1 (if open)
                                                                     │  message, sender's read marker,
                                                                     │  a push job per recipient
                                                                     ▼
other client ◀── ChatChanged {lastSeq} ── gateway ◀── ws.push ── chat
other client ── GET …/messages?after_seq=n ──▶ everything it has not seen
```

- **Messages are numbered per conversation, without gaps.** The seq comes from
  the same `update … returning` that checks the conversation is still open.
  That row lock serializes the sends in one conversation, and nothing lands
  after it has closed.
- **The socket carries doorbells only.** `ChatChanged` names the conversation
  and its newest seq, and the client reads everything after the newest seq it
  holds. A doorbell that arrives late, twice, out of order or not at all costs
  nothing, and the message has one shape: the proto's.
- **Sends are idempotent.** `client_message_id` (or `Idempotency-Key`) is
  unique per sender in a conversation. A retry returns the stored message, and
  it is answered before the rate limit is asked.
- **Read receipts are a marker per participant** that only moves forward.
  `ChatRead` goes out only when it moves.
- **Typing is `POST …/typing`**, throttled in memory to one doorbell per
  person per conversation every 3 seconds, and never stored. It goes over REST
  rather than the socket to keep gRPC calls out of the gateway's read loop.

## When a conversation exists

`TripAccepted` opens it, with the rider and that driver as participants.
`TripCompleted` or `TripCancelled` with a driver sets `closes_at` to the end
plus `CHAT_CLOSE_GRACE`, and the first end wins. After that it reads but does
not send. Openness is judged against the clock at each send, so there is no
sweeper.

The rider's app hears "accepted" straight from trip, usually before the fact
has come through the outbox. So `GET /v1/trips/{id}/conversation` asks trip
and creates the conversation itself when chat has not seen the fact yet. The
unique index on `trip_id` settles which of the two created it.

## Support

A rider or driver opens a support conversation, optionally about one of their
trips. Every connected ops user hears about it through the gateway's role
audience: the `ws.push` key `role:ops`.

Replying claims an unclaimed conversation, and a second agent is refused. The
claim is a compare-and-set on `assignee_id`. Resolving makes it read-only;
anything more is a new conversation. Support reads trip conversations and does
not post in them.

## Offline push

Each message owes every other participant a `chat_push_job`, due
`CHAT_PUSH_DELAY` (8s) later. The worker leases due jobs with
`for update skip locked`, so every replica runs one. It groups the jobs per
recipient and conversation, and drops a group whose recipient has read the
message in the meantime. Someone looking at the conversation needs no
notification, and nobody has to track who is looking.

A burst becomes one notification ("3 new messages"). A provider outage backs
off and gives up after five attempts. A `DeviceNotRegistered` ticket deletes
the token.

## Configuration

| variable             | default          |                                                            |
| -------------------- | ---------------- | ---------------------------------------------------------- |
| `DATABASE_URL`       | required         | no in-memory fallback                                      |
| `CHAT_PUSH_PROVIDER` | required         | `expo`, or `fake` which logs and notifies nobody           |
| `EXPO_ACCESS_TOKEN`  | empty            | only if the Expo project enables enhanced push security    |
| `CHAT_GRPC_LISTEN`   | `:8113`          |                                                            |
| `CHAT_METRICS_ADDR`  | `:9108`          |                                                            |
| `TRIP_GRPC_ADDR`     | `localhost:8110` |                                                            |
| `CHAT_GROUP`         | `chat`           | the `trip.lifecycle` consumer group                        |
| `CHAT_CLOSE_GRACE`   | `1h`             | how long after a trip ends its conversation takes messages |
| `CHAT_PUSH_DELAY`    | `8s`             | how long a message waits to be read before a notification  |
| `CHAT_RATE_LIMIT`    | `20`             | messages per person per minute                             |

`make dev-chat` runs it on the host with `CHAT_PUSH_PROVIDER=fake` unless
`.env` chooses otherwise.
