-- Profiles, owned by the core service: the first name and the photo a rider
-- and a driver see of each other.
--
-- Prefixed core_, as chat's tables are chat_, because the services share one
-- database.
create table if not exists core_profile (
  user_id       text primary key,
  -- As the person gave it. Empty until they do.
  display_name  text not null default '',
  -- Where the photo is in object storage, empty without one. Content-addressed,
  -- so a new photo is a new key and a link to the old one never shows the new.
  avatar_key    text not null default '',
  updated_at    timestamptz not null default now()
);
