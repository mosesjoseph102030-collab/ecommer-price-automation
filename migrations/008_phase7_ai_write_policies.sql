-- Phase 7 follow-up: restore tenant write access to the AI control tables.
--
-- Why this exists: migration 007 created ai_feature_flags and ai_kill_switches
-- with SELECT-only policies (ai_read_flags, ai_read_kill). Because those tables
-- are FORCE ROW LEVEL SECURITY, the NOBYPASSRLS application role could read the
-- effective AI state but could never write it, so both
--   PUT /app/{slug}/ai/features   and   PUT /app/{slug}/ai/kill-switch
-- failed with "new row violates row-level security policy" for every tenant.
-- Platform-scope writes kept working because pricing_worker is BYPASSRLS.
--
-- This migration is additive. 007 is already applied and is NOT edited, so
-- there is no drift between environments.
--
-- Security model, mirroring the pricing kill switch from migration 005:
--   - Read:  every tenant may observe the platform switch and its own switch.
--   - Write: a tenant may only ever create or modify rows for ITSELF.
--   - Platform-scope rows (organization_id IS NULL) are never writable by the
--     application role, so a store owner cannot disable or re-enable AI for
--     other tenants.
--   - The platform kill switch remains authoritative regardless, because
--     LoadControl evaluates platform scope before tenant scope.

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='ai_write_flags') THEN
    CREATE POLICY ai_write_flags ON ai_feature_flags FOR ALL
      USING (organization_id=current_setting('app.current_organization_id', true)::uuid)
      WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='ai_write_kill') THEN
    CREATE POLICY ai_write_kill ON ai_kill_switches FOR ALL
      USING (organization_id=current_setting('app.current_organization_id', true)::uuid)
      WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid);
  END IF;
END $$;
