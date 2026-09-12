-- Drivers, their vehicles and the paperwork behind both, owned by the fleet
-- service.
--
-- Prefixed fleet_, as chat's tables are chat_, because the services share one
-- database.

-- A driver's standing: whether they may be offered work, and why not.
--
-- One row per driver, created the first time they start onboarding. The
-- summary columns are derived from documents and vehicles rather than set by
-- hand, so a reviewer can never leave a driver approved with an expired
-- licence.
create table if not exists fleet_driver (
  driver_id            text primary key,
  -- 'onboarding', 'approved', 'blocked'. A driver is approved when their
  -- identity is verified, one vehicle is approved, and every required
  -- document is valid today.
  status               text not null default 'onboarding',
  -- Why a blocked driver is blocked, in a reviewer's words.
  blocked_reason       text not null default '',
  -- The Stripe Identity session, and what it decided.
  identity_status      text not null default 'unstarted',
  identity_session_id  text not null default '',
  -- What the identity check read off the licence, for a reviewer to compare
  -- against the documents.
  verified_name        text not null default '',
  verified_document    text not null default '',
  licence_expires_at   timestamptz,
  approved_at          timestamptz,
  created_at           timestamptz not null default now(),
  updated_at           timestamptz not null default now()
);

-- The queue a reviewer works from, and the sweeper's question: who is waiting.
create index if not exists fleet_driver_status on fleet_driver (status, updated_at);

-- Stripe's webhook names the session, not the driver.
create unique index if not exists fleet_driver_identity_session
  on fleet_driver (identity_session_id)
  where identity_session_id <> '';

-- A vehicle a driver offers to drive, as the RDW register describes it.
--
-- Everything but the plate and the ride class is copied from the register at
-- the moment it was read: a car whose APK lapses next month must stop being
-- offered work without anybody retyping its details.
create table if not exists fleet_vehicle (
  id                    text primary key,
  driver_id             text not null,
  -- Uppercase, no dashes, as the register holds it.
  plate                 text not null,
  make                  text not null default '',
  model                 text not null default '',
  colour                text not null default '',
  seats                 integer not null default 0,
  -- The ride class this car may serve, from trip's catalogue.
  package_slug          text not null default '',
  -- 'pending', 'approved', 'rejected', 'retired'.
  status                text not null default 'pending',
  rejected_reason       text not null default '',
  -- What the register said, and when we asked.
  apk_expires_at        timestamptz,
  taxi_registered       boolean not null default false,
  insured               boolean not null default false,
  first_registered_at   timestamptz,
  register_checked_at   timestamptz,
  created_at            timestamptz not null default now(),
  updated_at            timestamptz not null default now()
);

-- One live registration per plate: two drivers cannot both offer the same car.
create unique index if not exists fleet_vehicle_plate
  on fleet_vehicle (plate)
  where status <> 'retired';

create index if not exists fleet_vehicle_driver on fleet_vehicle (driver_id, status);

-- The sweeper's question: whose APK lapses next.
create index if not exists fleet_vehicle_apk on fleet_vehicle (apk_expires_at)
  where status = 'approved';

-- A document a driver has handed over, or is waiting on somebody else for.
--
-- The file itself lives in object storage; this holds the key. A document with
-- no key is one that exists as a state rather than a file — a VOG the driver
-- has applied for, a chauffeurskaart Kiwa has not posted yet.
create table if not exists fleet_document (
  id               text primary key,
  driver_id        text not null,
  -- The vehicle it belongs to, for insurance and registration. Empty for the
  -- documents that belong to the person.
  vehicle_id       text not null default '',
  -- 'insurance', 'registration', 'vog', 'chauffeurskaart'. The licence is not
  -- here: the identity check reads it, and storing a photo of it as well
  -- would be a copy nobody needs.
  kind             text not null,
  -- 'awaiting_file', 'awaiting_authority', 'submitted', 'approved',
  -- 'rejected', 'expired'. Only a reviewer moves a document to approved.
  status           text not null default 'awaiting_file',
  object_key       text not null default '',
  content_type     text not null default '',
  byte_size        bigint not null default 0,
  -- When the document stops being valid. A reviewer may correct what was read
  -- from it, which is why this is not derived.
  expires_at       timestamptz,
  -- What was read out of the file, as JSON: insurer, policy number, expiry,
  -- and where in the document each was found. Advice to a reviewer, never a
  -- decision.
  extracted        jsonb not null default '{}',
  -- 'none', 'pending', 'done', 'failed': whether a machine has read it yet.
  extraction       text not null default 'none',
  reviewer_id      text not null default '',
  review_note      text not null default '',
  reviewed_at      timestamptz,
  created_at       timestamptz not null default now(),
  updated_at       timestamptz not null default now()
);

create index if not exists fleet_document_driver on fleet_document (driver_id, kind);

-- The review queue: what is waiting for a person, oldest first.
create index if not exists fleet_document_queue
  on fleet_document (status, created_at)
  where status = 'submitted';

-- The sweeper's question: what lapses next.
create index if not exists fleet_document_expiry
  on fleet_document (expires_at)
  where status = 'approved';

-- What the fleet service tells the rest of the system, through the relay.
create table if not exists fleet_outbox (
  id          bigint generated always as identity primary key,
  topic       text not null,
  key         text not null,
  value       bytea not null,
  headers     jsonb not null default '{}',
  created_at  timestamptz not null default now()
);
