-- Trip state, owned by the trip service.
--
-- Idempotent and ledger-free, matching the convention on the TypeScript side:
-- applying the whole set to any database converges it on the committed schema,
-- so there is one rule across both languages rather than two.
--
-- The Go services own their own tables. packages/database owns better-auth's.
-- Neither writes to the other's, which is what keeps "one database, several
-- services" from quietly becoming a shared-database coupling.

create table if not exists trip (
  id               text primary key,
  rider_id         text not null,
  driver_id        text,
  status           text not null,

  pickup_lat       double precision not null,
  pickup_lng       double precision not null,
  dropoff_lat      double precision not null,
  dropoff_lng      double precision not null,

  polyline6        text not null default '',
  meters           double precision not null default 0,
  seconds          bigint not null default 0,

  total_cents      bigint not null default 0,
  surge_multiplier double precision not null default 1,
  package_slug     text not null default 'sedan',

  idempotency_key  text not null default '',

  created_at       timestamptz not null default now(),
  updated_at       timestamptz not null default now()
);

-- What actually enforces "book once".
--
-- The application checks for an existing trip first, but two requests racing
-- can both find nothing and both insert. Only the database can settle that, so
-- the uniqueness lives here and the application's check is an optimisation
-- rather than the guarantee.
--
-- Partial, because the empty string is not a key: trips created without one
-- must not collide with each other.
create unique index if not exists trip_rider_idempotency_key
  on trip (rider_id, idempotency_key)
  where idempotency_key <> '';

-- Riders read their own history, newest first.
create index if not exists trip_rider_created_at on trip (rider_id, created_at desc);

-- The dispatch console asks "what is happening right now", which is a query
-- over the few non-terminal trips among a table that is mostly finished ones.
create index if not exists trip_active on trip (status, created_at desc)
  where status not in ('completed', 'cancelled', 'unmatched');
