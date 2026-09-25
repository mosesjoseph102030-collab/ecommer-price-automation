-- Phase 7: AI assistance, hard-governed.
--
-- Non-negotiable design rule for this phase: AI output NEVER lands in a
-- business table. Every model response is written to ai_drafts as a proposal and
-- a human must confirm it. Business tables (products, costs, pricing_rules,
-- price_change_requests, ...) are written only by deterministic Go code after a
-- human decision.
--
-- The AI package does not import the woocommerce package, so it is structurally
-- incapable of calling a publish endpoint.

INSERT INTO permissions(id) VALUES
 ('ai.use'),('ai.approve'),('ai.configure')
ON CONFLICT (id) DO NOTHING;

-- ── Controls: feature flags, kill switch, quotas ───────────────────────────

-- Feature flags are per-feature and can be scoped platform-wide (NULL org) or
-- per tenant. AI capabilities must be feature-flagged, so nothing is callable
-- until a flag is explicitly enabled.
CREATE TABLE IF NOT EXISTS ai_feature_flags (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID REFERENCES organizations(id) ON DELETE CASCADE,
  feature TEXT NOT NULL CHECK (feature IN (
    'recommendation_explanation','invoice_ocr','rule_assistant','product_match_suggestion',
    'pricing_summary','feedback_clustering','community_summary','support_response_draft')),
  enabled BOOLEAN NOT NULL DEFAULT FALSE,
  config JSONB NOT NULL DEFAULT '{}',
  updated_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK ((organization_id IS NULL) <> (feature = 'community_summary' AND organization_id IS NOT NULL))
);
CREATE UNIQUE INDEX IF NOT EXISTS ai_feature_flag_scope_idx
  ON ai_feature_flags (feature, COALESCE(organization_id, '00000000-0000-0000-0000-000000000000'::uuid));

-- Instant AI kill switch. A platform kill switch overrides everything, including
-- per-feature flags and per-tenant enablement.
CREATE TABLE IF NOT EXISTS ai_kill_switches (
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
CREATE UNIQUE INDEX IF NOT EXISTS ai_kill_switch_scope_idx
  ON ai_kill_switches (scope, COALESCE(organization_id, '00000000-0000-0000-0000-000000000000'::uuid));

-- Per-tenant quotas. Enforced before any provider call.
CREATE TABLE IF NOT EXISTS ai_tenant_quotas (
  organization_id UUID PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
  daily_request_limit INT NOT NULL DEFAULT 50 CHECK (daily_request_limit > 0),
  daily_token_limit BIGINT NOT NULL DEFAULT 200000 CHECK (daily_token_limit > 0),
  max_input_bytes INT NOT NULL DEFAULT 60000 CHECK (max_input_bytes > 0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO ai_tenant_quotas (organization_id)
SELECT id FROM organizations ON CONFLICT (organization_id) DO NOTHING;

-- ── Audit: every invocation is logged ──────────────────────────────────────
-- Required fields: provider, prompt template version, record IDs, cost, outcome.
CREATE TABLE IF NOT EXISTS ai_invocations (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID REFERENCES organizations(id) ON DELETE SET NULL,
  feature TEXT NOT NULL,
  provider TEXT NOT NULL,
  model TEXT NOT NULL,
  prompt_template_version TEXT NOT NULL,
  record_ids JSONB NOT NULL DEFAULT '[]',
  input_redacted BOOLEAN NOT NULL DEFAULT FALSE,
  input_bytes INT NOT NULL DEFAULT 0 CHECK (input_bytes >= 0),
  input_tokens INT NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
  output_tokens INT NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
  cost_micros BIGINT NOT NULL DEFAULT 0 CHECK (cost_micros >= 0),
  outcome TEXT NOT NULL CHECK (outcome IN ('succeeded','schema_rejected','provider_error','blocked_kill_switch','blocked_flag','blocked_quota','blocked_untrusted_input','rate_limited')),
  error_code TEXT NOT NULL DEFAULT '',
  latency_ms BIGINT NOT NULL DEFAULT 0 CHECK (latency_ms >= 0),
  draft_id UUID,
  created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS ai_invocations_org_idx ON ai_invocations (organization_id, created_at DESC);
CREATE INDEX IF NOT EXISTS ai_invocations_feature_idx ON ai_invocations (feature, created_at DESC);
CREATE INDEX IF NOT EXISTS ai_invocations_outcome_idx ON ai_invocations (outcome, created_at DESC);

-- ── Drafts: the only place AI output is ever stored ────────────────────────
-- A draft is inert until a human approves it AND deterministic Go code has
-- re-validated and applied it.
CREATE TABLE IF NOT EXISTS ai_drafts (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  feature TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','rejected','applied','superseded')),
  -- Where this draft would apply. Not a foreign key: AI never writes here.
  target_type TEXT NOT NULL DEFAULT '',
  target_id TEXT NOT NULL DEFAULT '',
  -- Raw model output, retained for audit.
  raw_response TEXT NOT NULL DEFAULT '',
  -- Schema-validated payload. Any figures are recomputed by Go, not trusted.
  payload JSONB NOT NULL DEFAULT '{}',
  -- Figures the deterministic engine recomputed, overriding anything the model said.
  verified_payload JSONB NOT NULL DEFAULT '{}',
  record_ids JSONB NOT NULL DEFAULT '[]',
  provider TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL DEFAULT '',
  prompt_template_version TEXT NOT NULL DEFAULT '',
  created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  decided_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  decision_note TEXT NOT NULL DEFAULT '',
  decided_at TIMESTAMPTZ,
  applied_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS ai_drafts_org_idx ON ai_drafts (organization_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS ai_drafts_target_idx ON ai_drafts (target_type, target_id);

-- ── RLS ────────────────────────────────────────────────────────────────────
ALTER TABLE ai_feature_flags ENABLE ROW LEVEL SECURITY;
ALTER TABLE ai_feature_flags FORCE ROW LEVEL SECURITY;
ALTER TABLE ai_kill_switches ENABLE ROW LEVEL SECURITY;
ALTER TABLE ai_kill_switches FORCE ROW LEVEL SECURITY;
ALTER TABLE ai_tenant_quotas ENABLE ROW LEVEL SECURITY;
ALTER TABLE ai_tenant_quotas FORCE ROW LEVEL SECURITY;
ALTER TABLE ai_invocations ENABLE ROW LEVEL SECURITY;
ALTER TABLE ai_invocations FORCE ROW LEVEL SECURITY;
ALTER TABLE ai_drafts ENABLE ROW LEVEL SECURITY;
ALTER TABLE ai_drafts FORCE ROW LEVEL SECURITY;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='ai_tenant_scope_quotas') THEN
    CREATE POLICY ai_tenant_scope_quotas ON ai_tenant_quotas FOR ALL
      USING (organization_id=current_setting('app.current_organization_id', true)::uuid)
      WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='ai_tenant_scope_invocations') THEN
    CREATE POLICY ai_tenant_scope_invocations ON ai_invocations FOR ALL
      USING (organization_id=current_setting('app.current_organization_id', true)::uuid)
      WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='ai_tenant_scope_drafts') THEN
    CREATE POLICY ai_tenant_scope_drafts ON ai_drafts FOR ALL
      USING (organization_id=current_setting('app.current_organization_id', true)::uuid)
      WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid);
  END IF;
  -- Feature flags and kill switches are readable by a tenant (so the UI can show
  -- the effective state) but only ever writable by the controlled admin role.
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='ai_read_flags') THEN
    CREATE POLICY ai_read_flags ON ai_feature_flags FOR SELECT USING (true);
    CREATE POLICY ai_read_kill ON ai_kill_switches FOR SELECT USING (true);
  END IF;
END $$;
