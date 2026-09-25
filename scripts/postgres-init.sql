-- Development-only database roles. Production uses managed secrets and separate roles.
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='pricing_app') THEN
    CREATE ROLE pricing_app LOGIN PASSWORD 'pricing_app_dev_only' NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='pricing_worker') THEN
    CREATE ROLE pricing_worker LOGIN PASSWORD 'pricing_worker_dev_only' NOSUPERUSER NOCREATEDB NOCREATEROLE BYPASSRLS;
  END IF;
END $$;

GRANT USAGE ON SCHEMA public TO pricing_app, pricing_worker;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO pricing_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO pricing_worker;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO pricing_app, pricing_worker;
