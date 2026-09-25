-- Phase 4: approved-source competitor intelligence and human-reviewed matching.
-- No recommendations, approvals, WooCommerce publishing, AI, or billing.

INSERT INTO permissions(id) VALUES
 ('competitor.view'),('competitor.create'),('competitor.match.review')
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS competitors (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  source_type TEXT NOT NULL DEFAULT 'approved_web' CHECK (source_type IN ('approved_web','data_provider')),
  domain TEXT NOT NULL,
  location_market TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','paused','blocked')),
  monitoring_policy JSONB NOT NULL DEFAULT '{"interval_minutes":360,"freshness_minutes":720,"backoff_minutes":60}',
  created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (organization_id, domain)
);

CREATE TABLE IF NOT EXISTS competitor_products (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  competitor_id UUID NOT NULL REFERENCES competitors(id) ON DELETE CASCADE,
  url TEXT NOT NULL,
  name TEXT NOT NULL DEFAULT '',
  sku TEXT NOT NULL DEFAULT '',
  match_state TEXT NOT NULL DEFAULT 'unmatched' CHECK (match_state IN ('unmatched','suggested','needs_review','confirmed','rejected','stale','broken','paused')),
  suggested_product_id UUID REFERENCES products(id) ON DELETE SET NULL,
  confirmed_product_id UUID REFERENCES products(id) ON DELETE SET NULL,
  match_confidence_bps INT NOT NULL DEFAULT 0 CHECK (match_confidence_bps BETWEEN 0 AND 10000),
  last_observed_price_kobo BIGINT CHECK (last_observed_price_kobo >= 0),
  last_observed_at TIMESTAMPTZ,
  next_check_at TIMESTAMPTZ,
  consecutive_failures INT NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT '',
  created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (competitor_id, url)
);

CREATE TABLE IF NOT EXISTS competitor_match_reviews (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  competitor_product_id UUID NOT NULL REFERENCES competitor_products(id) ON DELETE CASCADE,
  product_id UUID REFERENCES products(id) ON DELETE SET NULL,
  previous_state TEXT NOT NULL,
  new_state TEXT NOT NULL CHECK (new_state IN ('needs_review','confirmed','rejected')),
  confidence_bps INT NOT NULL CHECK (confidence_bps BETWEEN 0 AND 10000),
  note TEXT NOT NULL DEFAULT '',
  reviewed_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS competitor_price_observations (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  competitor_product_id UUID NOT NULL REFERENCES competitor_products(id) ON DELETE CASCADE,
  source_url TEXT NOT NULL,
  observed_price_kobo BIGINT CHECK (observed_price_kobo >= 0),
  regular_price_kobo BIGINT CHECK (regular_price_kobo >= 0),
  sale_price_kobo BIGINT CHECK (sale_price_kobo >= 0),
  currency CHAR(3),
  availability TEXT NOT NULL DEFAULT 'unknown',
  extraction_confidence_bps INT NOT NULL CHECK (extraction_confidence_bps BETWEEN 0 AND 10000),
  raw_evidence_sha256 CHAR(64) NOT NULL,
  observed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  fresh_until TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS competitor_observations_product_idx ON competitor_price_observations (competitor_product_id, observed_at DESC);

CREATE TABLE IF NOT EXISTS competitor_evidence (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  observation_id UUID NOT NULL UNIQUE REFERENCES competitor_price_observations(id) ON DELETE CASCADE,
  raw_content TEXT NOT NULL,
  content_sha256 CHAR(64) NOT NULL,
  captured_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS competitor_alerts (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  competitor_product_id UUID REFERENCES competitor_products(id) ON DELETE CASCADE,
  alert_type TEXT NOT NULL CHECK (alert_type IN ('price_drop','stock_change','stale_data','broken_url','price_missing','currency_mismatch')),
  message TEXT NOT NULL,
  previous_value TEXT NOT NULL DEFAULT '',
  current_value TEXT NOT NULL DEFAULT '',
  acknowledged_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS competitor_alerts_org_idx ON competitor_alerts (organization_id, created_at DESC);

CREATE TABLE IF NOT EXISTS competitor_source_runs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  competitor_product_id UUID NOT NULL REFERENCES competitor_products(id) ON DELETE CASCADE,
  status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','running','succeeded','failed')),
  http_status INT,
  duration_ms BIGINT NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '',
  started_at TIMESTAMPTZ,
  finished_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS competitor_source_queue_idx ON competitor_source_runs (status, created_at);
CREATE INDEX IF NOT EXISTS competitor_products_due_idx ON competitor_products (next_check_at) WHERE match_state <> 'paused';

ALTER TABLE competitors ENABLE ROW LEVEL SECURITY;
ALTER TABLE competitors FORCE ROW LEVEL SECURITY;
ALTER TABLE competitor_products ENABLE ROW LEVEL SECURITY;
ALTER TABLE competitor_products FORCE ROW LEVEL SECURITY;
ALTER TABLE competitor_match_reviews ENABLE ROW LEVEL SECURITY;
ALTER TABLE competitor_match_reviews FORCE ROW LEVEL SECURITY;
ALTER TABLE competitor_price_observations ENABLE ROW LEVEL SECURITY;
ALTER TABLE competitor_price_observations FORCE ROW LEVEL SECURITY;
ALTER TABLE competitor_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE competitor_evidence FORCE ROW LEVEL SECURITY;
ALTER TABLE competitor_alerts ENABLE ROW LEVEL SECURITY;
ALTER TABLE competitor_alerts FORCE ROW LEVEL SECURITY;
ALTER TABLE competitor_source_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE competitor_source_runs FORCE ROW LEVEL SECURITY;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_competitors') THEN
    CREATE POLICY tenant_scope_competitors ON competitors FOR ALL USING (organization_id=current_setting('app.current_organization_id', true)::uuid) WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_competitor_products') THEN
    CREATE POLICY tenant_scope_competitor_products ON competitor_products FOR ALL USING (organization_id=current_setting('app.current_organization_id', true)::uuid) WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_match_reviews') THEN
    CREATE POLICY tenant_scope_match_reviews ON competitor_match_reviews FOR ALL USING (organization_id=current_setting('app.current_organization_id', true)::uuid) WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_competitor_observations') THEN
    CREATE POLICY tenant_scope_competitor_observations ON competitor_price_observations FOR ALL USING (organization_id=current_setting('app.current_organization_id', true)::uuid) WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_competitor_evidence') THEN
    CREATE POLICY tenant_scope_competitor_evidence ON competitor_evidence FOR ALL USING (organization_id=current_setting('app.current_organization_id', true)::uuid) WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_competitor_alerts') THEN
    CREATE POLICY tenant_scope_competitor_alerts ON competitor_alerts FOR ALL USING (organization_id=current_setting('app.current_organization_id', true)::uuid) WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_competitor_runs') THEN
    CREATE POLICY tenant_scope_competitor_runs ON competitor_source_runs FOR ALL USING (organization_id=current_setting('app.current_organization_id', true)::uuid) WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid);
  END IF;
END $$;
