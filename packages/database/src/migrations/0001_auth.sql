-- better-auth schema for the plugin set in apps/auth/src/iam/Options.ts:
-- emailAndPassword, emailVerification, google, magicLink, emailOTP, jwt.
--
-- Generated with `compileAuthMigrations` (better-auth 1.6.29) rather than
-- hand-written, then committed so deploys do not depend on the CLI. Every
-- statement is idempotent because there is no ledger: applying the whole set to
-- any database converges it on the committed schema, and
-- `test/Migrations.test.ts` holds that honest by applying it twice.
--
-- `jwks` is what makes the Go services work. better-auth signs short-lived
-- EdDSA tokens with the private key stored here and publishes the public half
-- at /api/auth/jwks, which every Go service fetches once and caches — so no
-- request path ever reaches this database for identity.

create table if not exists "user" ("id" text not null primary key, "name" text not null, "email" text not null unique, "emailVerified" boolean not null, "image" text, "createdAt" timestamptz default CURRENT_TIMESTAMP not null, "updatedAt" timestamptz default CURRENT_TIMESTAMP not null, "role" text);

create table if not exists "session" ("id" text not null primary key, "expiresAt" timestamptz not null, "token" text not null unique, "createdAt" timestamptz default CURRENT_TIMESTAMP not null, "updatedAt" timestamptz not null, "ipAddress" text, "userAgent" text, "userId" text not null references "user" ("id") on delete cascade);

create table if not exists "account" ("id" text not null primary key, "accountId" text not null, "providerId" text not null, "userId" text not null references "user" ("id") on delete cascade, "accessToken" text, "refreshToken" text, "idToken" text, "accessTokenExpiresAt" timestamptz, "refreshTokenExpiresAt" timestamptz, "scope" text, "password" text, "createdAt" timestamptz default CURRENT_TIMESTAMP not null, "updatedAt" timestamptz not null);

create table if not exists "verification" ("id" text not null primary key, "identifier" text not null, "value" text not null, "expiresAt" timestamptz not null, "createdAt" timestamptz default CURRENT_TIMESTAMP not null, "updatedAt" timestamptz default CURRENT_TIMESTAMP not null);

create table if not exists "jwks" ("id" text not null primary key, "publicKey" text not null, "privateKey" text not null, "createdAt" timestamptz not null, "expiresAt" timestamptz);

create index if not exists "session_userId_idx" on "session" ("userId");

create index if not exists "account_userId_idx" on "account" ("userId");

create index if not exists "verification_identifier_idx" on "verification" ("identifier");

-- A database-level backstop for the application default. better-auth writes the
-- column explicitly on every insert, so this only matters for a row created by
-- some other path — a fixture, a manual insert, an operator promoting somebody.
-- `rider` is the safe answer for all of them: `ops` is granted, never defaulted.
alter table "user" alter column "role" set default 'rider';
update "user" set "role" = 'rider' where "role" is null;
