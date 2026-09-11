-- Phone sign-in: the two columns better-auth's phoneNumber plugin adds to "user".
--
-- Generated with `compileAuthMigrations` (better-auth 1.6.29) against a
-- database at 0001, then guarded with `if not exists` like every migration
-- here, so applying the whole set to any database still converges it.
--
-- An account made from a phone number has a placeholder email on
-- `phone.surge.invalid`, because better-auth's user needs one — see
-- `@surge/domain/iam/Contact`, the one place that knows the convention.

alter table "user" add column if not exists "phoneNumber" text unique;

alter table "user" add column if not exists "phoneNumberVerified" boolean;
