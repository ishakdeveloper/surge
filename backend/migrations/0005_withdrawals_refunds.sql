-- A driver taking money out, and ops giving money back.

-- How much of a driver's share a refund took back. Kept beside net_cents, not
-- subtracted from it, so an earning still says what the trip earned and what
-- became of it; what a driver is actually due is the difference.
alter table earning add column if not exists reversed_cents bigint not null default 0;

create table if not exists refund (
  id                     text primary key,
  trip_id                text not null,
  payment_id             text not null,
  -- Given back to the rider.
  amount_cents           bigint not null,
  -- Taken back from the driver's share: their proportion of the refund.
  driver_cents           bigint not null default 0,
  currency               text not null,
  reason                 text not null default '',
  -- How the driver's part was taken back. none: nothing was earned.
  -- deducted: not paid out yet, so they are owed less. reversed: taken back
  -- from their balance. failed: their balance could not cover it, and the
  -- platform carries the loss.
  reversal               text not null,
  processor_refund_id    text not null default '',
  processor_reversal_id  text not null default '',
  idempotency_key        text not null,
  created_at             timestamptz not null default now()
);

-- One refund per key per trip: the retried click is the same refund.
create unique index if not exists refund_trip_key on refund (trip_id, idempotency_key);

create table if not exists withdrawal (
  id                   text primary key,
  driver_id            text not null,
  amount_cents         bigint not null,
  currency             text not null,
  -- requested: asked of the processor. in_transit: on its way to the bank.
  -- paid, failed: what the processor finally reported.
  status               text not null,
  processor_payout_id  text not null default '',
  failure_reason       text not null default '',
  idempotency_key      text not null,
  created_at           timestamptz not null default now(),
  updated_at           timestamptz not null default now()
);

-- The double tap on "withdraw" is one payout.
create unique index if not exists withdrawal_driver_key on withdrawal (driver_id, idempotency_key);

-- Payout webhooks name the processor's id.
create unique index if not exists withdrawal_payout
  on withdrawal (processor_payout_id)
  where processor_payout_id <> '';

create index if not exists withdrawal_driver_created on withdrawal (driver_id, created_at desc);
