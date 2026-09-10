-- What the trip service tells the rest of the system, written in the same
-- transaction as the change it describes.
--
-- The trip table and this one commit together or not at all, so a trip that
-- completed is a trip somebody hears completed — which is the property payments
-- depends on to charge for it. See shared/outbox.

create table if not exists trip_outbox (
  id          bigint generated always as identity primary key,
  topic       text not null,
  key         text not null,
  -- bytea rather than jsonb: jsonb reorders keys and normalises whitespace, and
  -- the relay should publish exactly the bytes the writer encoded.
  value       bytea not null,
  -- The writer's trace context, so the relay's produce joins its trace.
  headers     jsonb not null default '{}',
  created_at  timestamptz not null default now()
);

-- One market, one currency, but a price without a currency is a number, not an
-- amount. Stored per trip so a second market is a data change, not a migration
-- that has to guess what every existing row meant.
alter table trip add column if not exists currency text not null default 'eur';

-- Why a trip was cancelled. The service used to accept a reason and drop it,
-- and "payment_failed" is a reason the rider needs to be shown.
alter table trip add column if not exists cancel_reason text not null default '';
