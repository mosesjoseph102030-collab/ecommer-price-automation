-- ============================================================================
-- PostgreSQL bootstrap for the Pricing Intelligence platform.
--
-- Run this ONCE, as a superuser or the database owner, BEFORE running migrations.
-- It is deliberately kept OUTSIDE migrations/ because the migration runner
-- cannot create roles and this script is not idempotent DDL for the app schema.
--
--   psql -U <superuser> -d <dbname> -f scripts/postgres_bootstrap.sql
--
-- Why three roles?
--   pricing_migrator  owns the schema, applies DDL. Subject to RLS.
--   pricing_app       the API runtime role. NOBYPASSRLS: it is confined by
--                     row-level security and can only ever see its own tenant.
--   pricing_worker    background workers and platform admin. BYPASSRLS, so it
--                     can claim cross-tenant queue jobs. Never used by the API
--                     request path.
--
-- The API holds BOTH the app and worker connection strings, but only uses the
-- worker one for job claiming; every tenant-facing query runs on pricing_app.
-- ============================================================================

\set ON_ERROR_STOP on

-- ── Roles ───────────────────────────────────────────────────────────────────
-- Passwords below are placeholders. Change them before any shared deployment.
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pricing_app') THEN
    CREATE ROLE pricing_app LOGIN PASSWORD 'pricing_app_dev_only' NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pricing_worker') THEN
    -- BYPASSRLS is required: the publisher and competitor workers must claim
    -- queue rows across tenants, and they set the tenant context per job.
    CREATE ROLE pricing_worker LOGIN PASSWORD 'pricing_worker_dev_only' NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT BYPASSRLS;
  END IF;
END
$$;

ALTER ROLE pricing_app    NOSUPERUSER NOBYPASSRLS;
ALTER ROLE pricing_worker NOSUPERUSER BYPASSRLS;

-- ── Grants on the current database ──────────────────────────────────────────
DO $$
BEGIN
  EXECUTE format('GRANT CONNECT ON DATABASE %I TO pricing_app, pricing_worker', current_database());
END
$$;

-- PostgreSQL 15+ removed the implicit PUBLIC grant on schema public, so the
-- app roles must be granted usage explicitly or every query fails.
GRANT USAGE ON SCHEMA public TO pricing_app, pricing_worker;

-- Existing objects.
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO pricing_app, pricing_worker;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO pricing_app, pricing_worker;
GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA public TO pricing_app, pricing_worker;

-- Future objects. Without these, a new migration creates tables that the API
-- role cannot read, and the failure only appears at runtime.
--
-- NOTE: ALTER DEFAULT PRIVILEGES is per-role AND per-object-owner. It only
-- applies to objects created by the role that runs it. The grants below must be
-- executed while connected AS pricing_migrator (or whichever role owns the
-- schema and applies the migrations), otherwise they have no effect on the
-- tables that migration files create.
ALTER DEFAULT PRIVILEGES IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO pricing_app, pricing_worker;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO pricing_app, pricing_worker;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
  GRANT EXECUTE ON FUNCTIONS TO pricing_app, pricing_worker;

-- ── Verification ───────────────────────────────────────────────────────────
-- Run after migrations to confirm the security boundary is actually in place.
-- Every row below should be non-zero except the RLS counts you have not created.
--
-- SELECT rolname, rolsuper, rolbypassrls FROM pg_roles
--   WHERE rolname IN ('pricing_app','pricing_worker');
--
-- SELECT count(*) FROM pg_class c
--   JOIN pg_namespace n ON n.oid = c.relnamespace
--   WHERE n.nspname = 'public' AND c.relrowsecurity;      -- RLS enabled
--
-- SELECT count(*) FROM pg_class c
--   JOIN pg_namespace n ON n.oid = c.relnamespace
--   WHERE n.nspname = 'public' AND c.relforcerowsecurity; -- RLS forced
--
-- SELECT count(*) FROM pg_policies WHERE schemaname = 'public';
