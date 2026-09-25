-- Phase 5: recommendations, approvals, safe publishing, verification, and rollback.
-- No reports/community/feedback, AI, or billing.

INSERT INTO permissions(id) VALUES
 ('recommendation.view'),('recommendation.approve'),('recommendation.publish'),('recommendation.rollback')
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS pricing_kill_switches (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID REFERENCES organizations(id) ON DELETE CASCADE,
  scope TEXT NOT NULL CHECK (scope IN ('platform','tenant')),
  enabled BOOLEAN NOT NULL DEFAULT FALSE,
  reason TEXT NOT NULL DEFAULT '',
  changed_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK ((scope='platform' AND organization_id IS NULL) OR (scope='tenant' AND organization_id IS NOT NULL))
);
CREATE UNIQUE INDEX IF NOT EXISTS pricing_kill_switch_scope_idx ON pricing_kill_switches (scope, COALESCE(organization_id, '00000000-0000-0000-0000-000000000000'::uuid));

CREATE TABLE IF NOT EXISTS approval_limits (
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  role_id TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
  maximum_change_bps INT NOT NULL CHECK (maximum_change_bps BETWEEN 1 AND 10000),
  maximum_price_kobo BIGINT CHECK (maximum_price_kobo >= 0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (organization_id, role_id)
);
INSERT INTO approval_limits (organization_id, role_id, maximum_change_bps)
SELECT o.id, r.id, CASE WHEN r.id='store_owner' THEN 3000 ELSE 1000 END
FROM organizations o CROSS JOIN roles r WHERE r.id IN ('store_owner','pricing_manager')
ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS price_recommendations (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  generation_key TEXT NOT NULL,
  product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  variant_id UUID REFERENCES product_variants(id) ON DELETE CASCADE,
  state TEXT NOT NULL CHECK (state IN ('hold','raise','lower','investigate','pause')),
  previous_price_kobo BIGINT NOT NULL CHECK (previous_price_kobo >= 0),
  recommended_price_kobo BIGINT NOT NULL CHECK (recommended_price_kobo >= 0),
  minimum_profitable_price_kobo BIGINT NOT NULL CHECK (minimum_profitable_price_kobo >= 0),
  maximum_price_kobo BIGINT,
  lowest_confirmed_competitor_kobo BIGINT,
  change_bps INT NOT NULL,
  confidence_bps INT NOT NULL CHECK (confidence_bps BETWEEN 0 AND 10000),
  urgency TEXT NOT NULL DEFAULT 'normal' CHECK (urgency IN ('low','normal','high')),
  margin_risk TEXT NOT NULL DEFAULT 'none' CHECK (margin_risk IN ('none','warning','critical')),
  opportunity_kobo BIGINT,
  explanation TEXT NOT NULL DEFAULT '',
  expires_at TIMESTAMPTZ NOT NULL,
  generated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (organization_id, generation_key)
);
CREATE INDEX IF NOT EXISTS recommendations_inbox_idx ON price_recommendations (organization_id, state, generated_at DESC);

CREATE TABLE IF NOT EXISTS price_recommendation_reasons (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  recommendation_id UUID NOT NULL REFERENCES price_recommendations(id) ON DELETE CASCADE,
  reason_type TEXT NOT NULL,
  message TEXT NOT NULL,
  amount_kobo BIGINT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS price_change_requests (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  recommendation_id UUID NOT NULL REFERENCES price_recommendations(id) ON DELETE CASCADE,
  product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  previous_price_kobo BIGINT NOT NULL,
  requested_price_kobo BIGINT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('draft','pending','approved','rejected','expired','published','failed','rolled_back')),
  scheduled_for TIMESTAMPTZ,
  expires_at TIMESTAMPTZ NOT NULL,
  created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS active_request_recommendation_idx ON price_change_requests (recommendation_id) WHERE status IN ('pending','approved');

CREATE TABLE IF NOT EXISTS price_change_approvals (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  price_change_request_id UUID NOT NULL REFERENCES price_change_requests(id) ON DELETE CASCADE,
  approved_by_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  role_id TEXT NOT NULL,
  decision TEXT NOT NULL CHECK (decision IN ('approved','rejected')),
  note TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS price_change_executions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  price_change_request_id UUID NOT NULL REFERENCES price_change_requests(id) ON DELETE CASCADE,
  product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  idempotency_key TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','running','succeeded','failed','conflict','cancelled','rolled_back')),
  before_price_kobo BIGINT NOT NULL,
  requested_price_kobo BIGINT NOT NULL,
  verified_after_price_kobo BIGINT,
  attempts INT NOT NULL DEFAULT 0,
  scheduled_for TIMESTAMPTZ,
  last_error TEXT NOT NULL DEFAULT '',
  started_at TIMESTAMPTZ,
  finished_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (organization_id, idempotency_key)
);
CREATE INDEX IF NOT EXISTS price_execution_queue_idx ON price_change_executions (status, scheduled_for, created_at);

CREATE TABLE IF NOT EXISTS price_rollbacks (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  execution_id UUID NOT NULL REFERENCES price_change_executions(id) ON DELETE CASCADE,
  product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  restore_price_kobo BIGINT NOT NULL CHECK (restore_price_kobo >= 0),
  status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','running','succeeded','failed','conflict')),
  verified_after_price_kobo BIGINT,
  attempts INT NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT '',
  created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  finished_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS active_rollback_execution_idx ON price_rollbacks (execution_id) WHERE status IN ('queued','running');

ALTER TABLE pricing_kill_switches ENABLE ROW LEVEL SECURITY;
ALTER TABLE pricing_kill_switches FORCE ROW LEVEL SECURITY;
ALTER TABLE approval_limits ENABLE ROW LEVEL SECURITY;
ALTER TABLE approval_limits FORCE ROW LEVEL SECURITY;
ALTER TABLE price_recommendations ENABLE ROW LEVEL SECURITY;
ALTER TABLE price_recommendations FORCE ROW LEVEL SECURITY;
ALTER TABLE price_recommendation_reasons ENABLE ROW LEVEL SECURITY;
ALTER TABLE price_recommendation_reasons FORCE ROW LEVEL SECURITY;
ALTER TABLE price_change_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE price_change_requests FORCE ROW LEVEL SECURITY;
ALTER TABLE price_change_approvals ENABLE ROW LEVEL SECURITY;
ALTER TABLE price_change_approvals FORCE ROW LEVEL SECURITY;
ALTER TABLE price_change_executions ENABLE ROW LEVEL SECURITY;
ALTER TABLE price_change_executions FORCE ROW LEVEL SECURITY;
ALTER TABLE price_rollbacks ENABLE ROW LEVEL SECURITY;
ALTER TABLE price_rollbacks FORCE ROW LEVEL SECURITY;

DO $$ BEGIN
  -- Read policy: every tenant may observe the platform switch and its own switch.
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='kill_switch_read') THEN
    CREATE POLICY kill_switch_read ON pricing_kill_switches FOR SELECT
      USING (organization_id IS NULL OR organization_id=current_setting('app.current_organization_id', true)::uuid);
  END IF;
  -- Write policy: a tenant may only ever write its own row. Platform-scope rows
  -- are written exclusively through the controlled admin database role. This
  -- prevents a store owner from disabling publishing for other tenants.
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='kill_switch_write') THEN
    CREATE POLICY kill_switch_write ON pricing_kill_switches FOR ALL
      USING (organization_id=current_setting('app.current_organization_id', true)::uuid)
      WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_approval_limits') THEN CREATE POLICY tenant_scope_approval_limits ON approval_limits FOR ALL USING (organization_id=current_setting('app.current_organization_id', true)::uuid) WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid); END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_recommendations') THEN CREATE POLICY tenant_scope_recommendations ON price_recommendations FOR ALL USING (organization_id=current_setting('app.current_organization_id', true)::uuid) WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid); END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_recommendation_reasons') THEN CREATE POLICY tenant_scope_recommendation_reasons ON price_recommendation_reasons FOR ALL USING (organization_id=current_setting('app.current_organization_id', true)::uuid) WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid); END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_price_requests') THEN CREATE POLICY tenant_scope_price_requests ON price_change_requests FOR ALL USING (organization_id=current_setting('app.current_organization_id', true)::uuid) WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid); END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_price_approvals') THEN CREATE POLICY tenant_scope_price_approvals ON price_change_approvals FOR ALL USING (organization_id=current_setting('app.current_organization_id', true)::uuid) WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid); END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_price_executions') THEN CREATE POLICY tenant_scope_price_executions ON price_change_executions FOR ALL USING (organization_id=current_setting('app.current_organization_id', true)::uuid) WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid); END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_price_rollbacks') THEN CREATE POLICY tenant_scope_price_rollbacks ON price_rollbacks FOR ALL USING (organization_id=current_setting('app.current_organization_id', true)::uuid) WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid); END IF;
END $$;
