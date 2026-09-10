-- A rider's bank taking a charge back.
--
-- Stripe withdraws the disputed amount from the platform when the dispute
-- opens and returns it if the dispute is won. The platform is the merchant of
-- record, so the dispute is its to fight; what this table records is what
-- happened to the driver's share of the money while it did.

create table if not exists dispute (
  id                             text primary key,
  processor_dispute_id           text not null unique,
  trip_id                        text not null,
  payment_id                     text not null,
  amount_cents                   bigint not null,
  -- The driver's share of the disputed amount, taken back while it is open.
  driver_cents                   bigint not null default 0,
  currency                       text not null,
  reason                         text not null default '',
  -- How the driver's share was taken back, as for a refund: none, deducted,
  -- reversed, or failed (already withdrawn; the platform carries it).
  reversal                       text not null,
  -- open, won or lost.
  status                         text not null,
  processor_reversal_id          text not null default '',
  -- The transfer that gave a reversed share back when the dispute was won.
  processor_restore_transfer_id  text not null default '',
  created_at                     timestamptz not null default now(),
  updated_at                     timestamptz not null default now()
);

create index if not exists dispute_trip on dispute (trip_id);
