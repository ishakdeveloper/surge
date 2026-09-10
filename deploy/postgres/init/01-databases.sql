-- Two databases, because there are two owners.
--
-- `surge_auth` holds better-auth's tables — user, session, account,
-- verification and the jwks keypair. `surge` holds everything the Go services
-- own. Nothing joins across them, and now nothing can: a cross-service query is
-- not rude, it is impossible.
--
-- That is the point of the split rather than two schemas in one database. A
-- schema boundary is enforced by grants somebody has to maintain; a database
-- boundary is enforced by there being no connection. It also means the auth
-- database can move to a different instance, a different region or a managed
-- provider without a single line of application code changing.
--
-- The cost is honest: two connection strings, two backup stories, and two
-- things to provision. It buys an isolation that cannot quietly erode.
--
-- Run once, by the postgres image's initdb hook. `create database` cannot be
-- made idempotent with IF NOT EXISTS, so this file is not re-runnable — unlike
-- every migration in the project, which is. initdb only ever runs on an empty
-- data directory, so that is fine here and nowhere else.
CREATE DATABASE surge_auth;

COMMENT ON DATABASE surge IS 'Trip, and everything else the Go services own.';
COMMENT ON DATABASE surge_auth IS 'better-auth only. No Go service connects here.';
