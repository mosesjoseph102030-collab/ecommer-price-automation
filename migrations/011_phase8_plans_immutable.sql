-- Phase 8 follow-up: plans held prices with a SELECT policy but no RLS enabled.
--
-- A policy with RLS switched off is inert, so the `pricing_app` role kept full
-- DML on the plan catalogue through the default privileges. That means any SQL
-- injection in the API could rewrite a plan's price or entitlements and grant
-- itself unlimited quota. Plan prices are the one thing in this schema that must
-- be immutable to the application.
--
-- The read policy already exists from 010; this enables and forces RLS so the
-- application role can read plans and nothing else. Writes remain the exclusive
-- right of the migration role, which is also the role that seeds them.

ALTER TABLE plans ENABLE ROW LEVEL SECURITY;
ALTER TABLE plans FORCE ROW LEVEL SECURITY;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='plans_read_only') THEN
    CREATE POLICY plans_read_only ON plans FOR SELECT USING (true);
  END IF;
  -- No INSERT/UPDATE/DELETE policy exists on purpose. With RLS forced and no
  -- write policy, every write is rejected for pricing_app, which is the desired
  -- outcome: a plan is changed by a migration, never by a request.
END $$;

-- The operational tables added in 010 were given read-only policies, but their
-- RLS is only now verified as enabled and forced in the same migration. This
-- block is intentionally a no-op re-assertion so a database restored from a
-- partial 010 ends up in the same state as a clean install.
ALTER TABLE circuit_breaker_state ENABLE ROW LEVEL SECURITY;
ALTER TABLE circuit_breaker_state FORCE ROW LEVEL SECURITY;
ALTER TABLE incidents ENABLE ROW LEVEL SECURITY;
ALTER TABLE incidents FORCE ROW LEVEL SECURITY;
ALTER TABLE retention_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE retention_runs FORCE ROW LEVEL SECURITY;
ALTER TABLE dead_letter_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE dead_letter_jobs FORCE ROW LEVEL SECURITY;
ALTER TABLE billing_webhook_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE billing_webhook_events FORCE ROW LEVEL SECURITY;
