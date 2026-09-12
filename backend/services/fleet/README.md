# fleet

Who may drive, and what they may drive.

## Why it is separate

It holds two credentials the rest of the system does not: a Stripe key for
identity checks, and the documents bucket's. It also calls three things outside
the cluster — Stripe Identity, the Dutch vehicle register, and a model that
reads certificates — and none of them may sit in front of a dispatch.

The payments service still holds the credentials that move money. This one's
Stripe key should be a **restricted key with Identity and nothing else**, which
is the difference between verifying a driver and being able to pay them.

Nothing calls this service to ask whether a driver may work. It publishes that
on `fleet.drivers`, keyed by driver and compacted, so whoever dispatches reads
the current standing of everyone from the start of the topic.

Whoever dispatches is the matcher. With `MATCHER_REQUIRE_APPROVAL=true` every
instance follows the whole topic — sixteen partitions of standings do not
co-partition with thirty-two of geography, so nothing about it can live in a
shard — and refuses to offer a trip to anyone not approved on it. A withdrawal
therefore takes effect at the next dispatch decision, not at the next
deployment, and a driver mid-trip keeps the trip they are on.

## Approval is derived, never remembered

```
identity verified ─┐
a vehicle approved ─┼─▶ approved
every document valid ─┘
```

`domain.Outstanding` works out what a driver still owes, every time it is
asked, from their papers. That is what keeps somebody from staying approved
with a licence that lapsed in March: the licence's own expiry is one of the
things it looks at, alongside the car's inspection date and each document's.

A reviewer can block a driver regardless, and a block outlives any amount of
perfect paperwork until it is lifted.

## What each integration answers

|                                                 |                                                                                                                                                                                                                              |
| ----------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **RDW open data** (`m9d7-ebf2`)                 | free, no key, no account. A plate returns make, model, colour, seats, **APK expiry**, **taxi registration** and **whether an insurer has a policy against it**. A driver types six characters instead of a form.             |
| **Stripe Identity**                             | the licence and a selfie, with live capture on. It returns the holder's name and the licence's expiry, which is why no photograph of a licence is stored here — the most sensitive document in the set is read and not kept. |
| **Claude, with vision**                         | reads an insurance certificate: insurer, policy number, expiry, and the sentences it took them from. Advice to a reviewer and never a decision; a reading that fails changes nothing.                                        |
| **Justis (VOG)** and **Kiwa (chauffeurskaart)** | have no API anywhere, by design. Both take weeks and are decided by a person, so they are states a driver moves through with evidence attached.                                                                              |

Refusals the register can make are made without a person: no such plate, a
lapsed inspection, no insurance, too few seats for the class. What it cannot
decide becomes a note for the reviewer — a car not registered for taxi use is a
conversation, not a rejection.

## Documents never pass through this API

`StartUpload` returns a presigned PUT, and the app sends the file straight to
the bucket. The link signs the content type and length, so a link issued for a
2 MB certificate cannot take a gigabyte of something else. `FinishUpload` says
it arrived, which is when the reading starts and the document reaches the
queue.

A reviewer's link to a file is presigned too, and its signing time is rounded
down to five minutes, so refreshing the page does not mint a new URL.

## The review queue

Ordered by the oldest document still waiting rather than by when the driver
signed up, so somebody who sent one more certificate this morning has not gone
to the back of the queue. A decision needs an expiry date read off the document
when approving, and a reason when refusing — the driver reads the reason.

## Configuration

| variable                         | default           |                                                                 |
| -------------------------------- | ----------------- | --------------------------------------------------------------- |
| `DATABASE_URL`                   | required          | no in-memory fallback                                           |
| `FLEET_IDENTITY_PROVIDER`        | required          | `stripe`, or `fake` which verifies whoever asks                 |
| `STRIPE_IDENTITY_KEY`            |                   | a restricted key, Identity only                                 |
| `STRIPE_IDENTITY_WEBHOOK_SECRET` |                   | its own endpoint, its own secret                                |
| `FLEET_DOCUMENT_STORE`           | required          | `r2`, or `memory` which keeps nothing                           |
| `FLEET_DOCUMENT_BUCKET`          | `surge-documents` |                                                                 |
| `FLEET_REGISTER`                 | `rdw`             | the real register needs no key; `fake` answers from five plates |
| `RDW_APP_TOKEN`                  | empty             | optional; without one the register throttles by IP              |
| `FLEET_READER`                   | `off`             | `claude`, `fake` or `off`                                       |
| `FLEET_SWEEP_INTERVAL`           | `1h`              | how often lapsed papers are swept up                            |
| `FLEET_GRPC_LISTEN`              | `:8115`           |                                                                 |
| `FLEET_METRICS_ADDR`             | `:9110`           |                                                                 |

`make dev-fleet` runs it with identity and storage faked, against the real
vehicle register.
