# payments

Owns money: the hold placed on a rider's card when a trip is requested, the
capture when it completes, the release when it does not, and the driver's share
passed on to their connected account.

It is separate for isolation, not scale. It is the only process with processor
credentials and the only one calling an external money API, and that API's
latency, outages and rate limits must never stall the trip state machine. So the
trip service never calls it: trip publishes `trip.lifecycle` facts through an
outbox, and payments answers on `payment.events` the same way.

## What a trip's facts do

| fact on `trip.lifecycle` | payments                                                             |
| ------------------------ | -------------------------------------------------------------------- |
| `TripRequested`          | hold the fare on the rider's card (a manual-capture PaymentIntent)   |
| `TripCompleted`          | capture it, record the driver's earning, transfer it if they can receive |
| `TripCancelled`          | release the hold                                                     |
| `TripUnmatched`          | release the hold                                                     |

and what it says back on `payment.events`: `PaymentAuthorized`,
`PaymentActionRequired` (a 3-D Secure challenge), `PaymentFailed` with a reason,
`PaymentCaptured`.

## What it guarantees

- **One hold, one capture, one transfer per trip**, however often a fact is
  delivered. Every processor call carries a key derived from the trip, the
  attempt and the operation — `trip:{id}:{attempt}:capture` — never a random
  one, so a retry after a crash gets the first call's answer.
- **A change and everything that follows from it commit together.** A capture,
  the driver's earning, its ledger entries and the `PaymentCaptured` fact are
  one transaction (`domain.Change`), with a compare-and-set on the status the
  change was decided against.
- **Money is never created.** The ledger is double entry; every transaction
  sums to zero, and a transaction posted twice is a constraint violation.
- **A fact is never skipped because something was down.** Transient failures
  are retried in place, blocking the partition: lag climbs and somebody notices,
  rather than a ride going quietly unpaid.

## Layout

    cmd/                                entrypoint and wiring
    internal/domain/                    payment state machine, ledger, earnings
    internal/service/                   the lifecycle, over a Processor port
    internal/infrastructure/fake/       a deterministic processor, Stripe's test cards
    internal/infrastructure/repository/ Postgres (and in-memory for tests)
    internal/infrastructure/events/     trip.lifecycle consumer

## Ports

|         |       |
| ------- | ----- |
| metrics | :9107 |

## Configuration

| variable                  | default    |                                                            |
| ------------------------- | ---------- | ---------------------------------------------------------- |
| `DATABASE_URL`            | (required) | no in-memory fallback, on purpose                          |
| `PAYMENTS_PROCESSOR`      | `stripe`   | `fake` simulates everything and charges nobody             |
| `PAYMENTS_COMMISSION_BPS` | `2000`     | the platform's share, in basis points                      |
| `PAYMENTS_GROUP`          | `payments` | consumer group on `trip.lifecycle`                         |
| `PAYMENTS_METRICS_ADDR`   | `:9107`    |                                                            |

Read through `shared/config`, which fails loudly: an unset variable may take a
default, a set-but-unparseable one is always an error.
