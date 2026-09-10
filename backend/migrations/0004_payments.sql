-- Money, owned by the payments service.
--
-- Columns name the processor generically — processor_payment_id rather than
-- stripe_payment_intent_id — because the fake processor fills them too, and a
-- column called stripe_* holding a fake id is a column that lies. The values in
-- production are Stripe ids (pi_…, ch_…, acct_…, tr_…).

-- A rider as the processor knows them, and the card that will be held.
create table if not exists payment_customer (
  user_id                text primary key,
  processor_customer_id  text not null unique,
  payment_method_id      text not null default '',
  card_brand             text not null default '',
  card_last4             text not null default '',
  card_exp_month         integer not null default 0,
  card_exp_year          integer not null default 0,
  created_at             timestamptz not null default now(),
  updated_at             timestamptz not null default now()
);

-- One payment per trip: the hold placed when it was requested, and what became
-- of it.
create table if not exists payment (
  id                    text primary key,
  trip_id               text not null,
  -- Attempt numbers a trip's holds. A trip re-requested after its hold was
  -- released needs a new one, and the processor idempotency key carries the
  -- attempt so the retry is a new request rather than a replay of the old.
  attempt               integer not null default 1,
  rider_id              text not null,
  driver_id             text not null default '',
  status                text not null,

  amount_cents          bigint not null,
  captured_cents        bigint not null default 0,
  refunded_cents        bigint not null default 0,
  commission_cents      bigint not null default 0,
  currency              text not null,

  processor_payment_id  text not null default '',
  processor_charge_id   text not null default '',
  -- Held only while the rider has an authentication step to finish, and
  -- cleared once they have: it confirms this payment and nothing else, but it
  -- has no business outliving its use.
  client_secret         text not null default '',
  failure_reason        text not null default '',

  created_at            timestamptz not null default now(),
  updated_at            timestamptz not null default now()
);

create unique index if not exists payment_trip on payment (trip_id);

-- Webhooks name the processor's id, not ours.
create unique index if not exists payment_processor_payment
  on payment (processor_payment_id)
  where processor_payment_id <> '';

create index if not exists payment_rider_created on payment (rider_id, created_at desc);

-- The sweeper's question: which holds are still open, and since when.
create index if not exists payment_open on payment (status, updated_at)
  where status in ('authorizing', 'requires_action', 'authorized');

-- A driver's connected account, and whether it can receive money yet.
create table if not exists payout_account (
  driver_id             text primary key,
  processor_account_id  text not null unique,
  -- The processor's capability status for receiving transfers, mirrored from
  -- its webhooks. Only 'active' may be paid.
  transfers_status      text not null default 'pending',
  requirements_due      boolean not null default true,
  created_at            timestamptz not null default now(),
  updated_at            timestamptz not null default now()
);

-- What a driver earned on a trip, and whether it has been paid to them.
create table if not exists earning (
  trip_id                text primary key,
  driver_id              text not null,
  payment_id             text not null,
  gross_cents            bigint not null,
  commission_cents       bigint not null,
  net_cents              bigint not null,
  currency               text not null,
  -- unpaid: owed, waiting for an account that can receive it.
  -- transferred: moved to the driver's account.
  -- reversed: taken back after a refund or dispute.
  status                 text not null,
  processor_transfer_id  text not null default '',
  created_at             timestamptz not null default now(),
  updated_at             timestamptz not null default now()
);

create index if not exists earning_driver_created on earning (driver_id, created_at desc);

-- The sweep when a driver's account becomes able to receive money.
create index if not exists earning_unpaid on earning (driver_id) where status = 'unpaid';

-- Double entry. Every transaction's entries sum to zero, which is the property
-- that makes "where did this euro go" a query rather than an investigation.
-- Positive is a debit, negative a credit.
create table if not exists ledger_entry (
  id            bigint generated always as identity primary key,
  txn_id        text not null,
  kind          text not null,
  account       text not null,
  amount_cents  bigint not null,
  currency      text not null,
  trip_id       text not null default '',
  created_at    timestamptz not null default now()
);

-- One entry per account per transaction, so a transaction applied twice is a
-- constraint violation rather than money counted twice.
create unique index if not exists ledger_entry_txn_account on ledger_entry (txn_id, account);
create index if not exists ledger_entry_account on ledger_entry (account, created_at desc);

-- Webhook events already handled. The processor delivers at least once, and
-- inserting here first is what makes the second delivery a no-op.
create table if not exists processor_event (
  event_id     text primary key,
  type         text not null,
  received_at  timestamptz not null default now()
);

-- Facts for payment.events, written in the same transaction as the change.
create table if not exists payments_outbox (
  id          bigint generated always as identity primary key,
  topic       text not null,
  key         text not null,
  value       bytea not null,
  headers     jsonb not null default '{}',
  created_at  timestamptz not null default now()
);
