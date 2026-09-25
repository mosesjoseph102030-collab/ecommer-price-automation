-- Phase 2: WooCommerce connection + catalog sync. No costs/rules/competitors.

-- Stores belong to an organization (tenant).
CREATE TABLE IF NOT EXISTS stores (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  url TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','disconnected','archived')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (organization_id, url)
);

CREATE TABLE IF NOT EXISTS store_connections (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  store_id UUID NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
  api_version TEXT NOT NULL DEFAULT 'v3',
  status TEXT NOT NULL DEFAULT 'connected' CHECK (status IN ('connected','revoked','error','disconnected')),
  last_sync_at TIMESTAMPTZ,
  last_error TEXT NOT NULL DEFAULT '',
  webhook_status TEXT NOT NULL DEFAULT 'unknown',
  rate_limited_until TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (organization_id, store_id)
);

-- Encrypted credentials only. Plaintext never stored; secrets never returned by API.
CREATE TABLE IF NOT EXISTS store_connection_credentials (
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  connection_id UUID PRIMARY KEY REFERENCES store_connections(id) ON DELETE CASCADE,
  ciphertext BYTEA NOT NULL,
  nonce BYTEA NOT NULL,
  key_version INT NOT NULL DEFAULT 1,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS store_sync_runs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  store_id UUID NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('initial','webhook','reconcile','retry')),
  status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','running','succeeded','partial','failed')),
  imported_products INT NOT NULL DEFAULT 0,
  imported_variants INT NOT NULL DEFAULT 0,
  failed_items INT NOT NULL DEFAULT 0,
  error_summary TEXT NOT NULL DEFAULT '',
  attempts INT NOT NULL DEFAULT 0,
  next_attempt_at TIMESTAMPTZ,
  requested_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  started_at TIMESTAMPTZ,
  finished_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS sync_runs_store_idx ON store_sync_runs(store_id, created_at DESC);
CREATE INDEX IF NOT EXISTS sync_runs_queue_idx ON store_sync_runs(status, next_attempt_at, created_at);

CREATE TABLE IF NOT EXISTS store_sync_items (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  sync_run_id UUID NOT NULL REFERENCES store_sync_runs(id) ON DELETE CASCADE,
  external_id TEXT NOT NULL DEFAULT '',
  sku TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'ok' CHECK (status IN ('ok','failed','skipped')),
  error TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Raw webhook ingress: stored before processing, deduplicated by provider event id.
CREATE TABLE IF NOT EXISTS webhook_events (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID REFERENCES organizations(id) ON DELETE CASCADE,
  store_connection_id UUID REFERENCES store_connections(id) ON DELETE SET NULL,
  provider_event_id TEXT NOT NULL DEFAULT '',
  topic TEXT NOT NULL DEFAULT '',
  raw_body BYTEA NOT NULL,
  signature TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'received' CHECK (status IN ('received','processed','duplicate','failed')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (store_connection_id, provider_event_id)
);

CREATE TABLE IF NOT EXISTS webhook_delivery_attempts (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  webhook_event_id UUID NOT NULL REFERENCES webhook_events(id) ON DELETE CASCADE,
  attempt INT NOT NULL DEFAULT 1,
  status TEXT NOT NULL DEFAULT 'queued',
  error TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Catalog (normalized from WooCommerce).
CREATE TABLE IF NOT EXISTS product_categories (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  external_id TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL,
  slug TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (organization_id, external_id)
);

CREATE TABLE IF NOT EXISTS products (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  store_id UUID NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
  external_id TEXT NOT NULL DEFAULT '',
  sku TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'published' CHECK (status IN ('draft','private','published','archived','deleted')),
  stock_status TEXT NOT NULL DEFAULT 'instock',
  stock_quantity INT,
  price_kobo BIGINT NOT NULL DEFAULT 0 CHECK (price_kobo >= 0),
  sale_price_kobo BIGINT,
  platform_price_kobo BIGINT,
  price_conflict BOOLEAN NOT NULL DEFAULT FALSE,
  price_conflict_detected_at TIMESTAMPTZ,
  needs_review BOOLEAN NOT NULL DEFAULT FALSE,
  review_reason TEXT NOT NULL DEFAULT '',
  tax_class TEXT NOT NULL DEFAULT '',
  woo_updated_at TIMESTAMPTZ,
  last_sync_run_id UUID REFERENCES store_sync_runs(id) ON DELETE SET NULL,
  last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (organization_id, external_id)
);
CREATE INDEX IF NOT EXISTS products_store_idx ON products(store_id);

CREATE TABLE IF NOT EXISTS product_variants (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  external_id TEXT NOT NULL DEFAULT '',
  sku TEXT NOT NULL DEFAULT '',
  price_kobo BIGINT NOT NULL DEFAULT 0 CHECK (price_kobo >= 0),
  sale_price_kobo BIGINT,
  stock_status TEXT NOT NULL DEFAULT 'instock',
  stock_quantity INT,
  attributes JSONB NOT NULL DEFAULT '{}',
  mapping_conflict BOOLEAN NOT NULL DEFAULT FALSE,
  mapping_conflict_detail TEXT NOT NULL DEFAULT '',
  woo_updated_at TIMESTAMPTZ,
  last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (organization_id, external_id)
);

CREATE TABLE IF NOT EXISTS product_category_memberships (
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  category_id UUID NOT NULL REFERENCES product_categories(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (product_id, category_id)
);

CREATE TABLE IF NOT EXISTS product_external_mappings (
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  external_product_id TEXT NOT NULL,
  external_variant_id TEXT NOT NULL DEFAULT '',
  product_id UUID REFERENCES products(id) ON DELETE SET NULL,
  variant_id UUID REFERENCES product_variants(id) ON DELETE SET NULL,
  conflict TEXT NOT NULL DEFAULT '' ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (organization_id, external_product_id, external_variant_id)
);

CREATE TABLE IF NOT EXISTS product_price_snapshots (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  product_id UUID REFERENCES products(id) ON DELETE CASCADE,
  variant_id UUID REFERENCES product_variants(id) ON DELETE CASCADE,
  price_kobo BIGINT NOT NULL,
  source TEXT NOT NULL DEFAULT 'woo' CHECK (source IN ('woo','platform')),
  observed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS price_snapshots_product_idx ON product_price_snapshots(product_id, observed_at DESC);

CREATE TABLE IF NOT EXISTS product_stock_snapshots (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  product_id UUID REFERENCES products(id) ON DELETE CASCADE,
  quantity INT,
  stock_status TEXT NOT NULL DEFAULT '',
  observed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- RLS on tenant tables.
ALTER TABLE stores ENABLE ROW LEVEL SECURITY;
ALTER TABLE stores FORCE ROW LEVEL SECURITY;
ALTER TABLE store_connections ENABLE ROW LEVEL SECURITY;
ALTER TABLE store_connections FORCE ROW LEVEL SECURITY;
ALTER TABLE store_connection_credentials ENABLE ROW LEVEL SECURITY;
ALTER TABLE store_connection_credentials FORCE ROW LEVEL SECURITY;
ALTER TABLE store_sync_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE store_sync_runs FORCE ROW LEVEL SECURITY;
ALTER TABLE webhook_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhook_events FORCE ROW LEVEL SECURITY;
ALTER TABLE webhook_delivery_attempts ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhook_delivery_attempts FORCE ROW LEVEL SECURITY;
ALTER TABLE store_sync_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE store_sync_items FORCE ROW LEVEL SECURITY;
ALTER TABLE product_categories ENABLE ROW LEVEL SECURITY;
ALTER TABLE product_categories FORCE ROW LEVEL SECURITY;
ALTER TABLE products ENABLE ROW LEVEL SECURITY;
ALTER TABLE products FORCE ROW LEVEL SECURITY;
ALTER TABLE product_variants ENABLE ROW LEVEL SECURITY;
ALTER TABLE product_variants FORCE ROW LEVEL SECURITY;
ALTER TABLE product_category_memberships ENABLE ROW LEVEL SECURITY;
ALTER TABLE product_category_memberships FORCE ROW LEVEL SECURITY;
ALTER TABLE product_external_mappings ENABLE ROW LEVEL SECURITY;
ALTER TABLE product_external_mappings FORCE ROW LEVEL SECURITY;
ALTER TABLE product_price_snapshots ENABLE ROW LEVEL SECURITY;
ALTER TABLE product_price_snapshots FORCE ROW LEVEL SECURITY;
ALTER TABLE product_stock_snapshots ENABLE ROW LEVEL SECURITY;
ALTER TABLE product_stock_snapshots FORCE ROW LEVEL SECURITY;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_stores') THEN
    CREATE POLICY tenant_scope_stores ON stores FOR ALL
    USING (organization_id = current_setting('app.current_organization_id', true)::uuid)
    WITH CHECK (organization_id = current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_connections') THEN
    CREATE POLICY tenant_scope_connections ON store_connections FOR ALL
    USING (organization_id = current_setting('app.current_organization_id', true)::uuid)
    WITH CHECK (organization_id = current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_credentials') THEN
    CREATE POLICY tenant_scope_credentials ON store_connection_credentials FOR ALL
    USING (organization_id = current_setting('app.current_organization_id', true)::uuid)
    WITH CHECK (organization_id = current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_syncruns') THEN
    CREATE POLICY tenant_scope_syncruns ON store_sync_runs FOR ALL
    USING (organization_id = current_setting('app.current_organization_id', true)::uuid)
    WITH CHECK (organization_id = current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_syncitems') THEN
    CREATE POLICY tenant_scope_syncitems ON store_sync_items FOR ALL
    USING (organization_id = current_setting('app.current_organization_id', true)::uuid)
    WITH CHECK (organization_id = current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_webhooks') THEN
    CREATE POLICY tenant_scope_webhooks ON webhook_events FOR ALL
    USING (organization_id = current_setting('app.current_organization_id', true)::uuid)
    WITH CHECK (organization_id = current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_delivery_attempts') THEN
    CREATE POLICY tenant_scope_delivery_attempts ON webhook_delivery_attempts FOR ALL
    USING (organization_id = current_setting('app.current_organization_id', true)::uuid)
    WITH CHECK (organization_id = current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_categories') THEN
    CREATE POLICY tenant_scope_categories ON product_categories FOR ALL
    USING (organization_id = current_setting('app.current_organization_id', true)::uuid)
    WITH CHECK (organization_id = current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_products') THEN
    CREATE POLICY tenant_scope_products ON products FOR ALL
    USING (organization_id = current_setting('app.current_organization_id', true)::uuid)
    WITH CHECK (organization_id = current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_variants') THEN
    CREATE POLICY tenant_scope_variants ON product_variants FOR ALL
    USING (organization_id = current_setting('app.current_organization_id', true)::uuid)
    WITH CHECK (organization_id = current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_category_memberships') THEN
    CREATE POLICY tenant_scope_category_memberships ON product_category_memberships FOR ALL
    USING (organization_id = current_setting('app.current_organization_id', true)::uuid)
    WITH CHECK (organization_id = current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_mappings') THEN
    CREATE POLICY tenant_scope_mappings ON product_external_mappings FOR ALL
    USING (organization_id = current_setting('app.current_organization_id', true)::uuid)
    WITH CHECK (organization_id = current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_pricesnap') THEN
    CREATE POLICY tenant_scope_pricesnap ON product_price_snapshots FOR ALL
    USING (organization_id = current_setting('app.current_organization_id', true)::uuid)
    WITH CHECK (organization_id = current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_stocksnap') THEN
    CREATE POLICY tenant_scope_stocksnap ON product_stock_snapshots FOR ALL
    USING (organization_id = current_setting('app.current_organization_id', true)::uuid)
    WITH CHECK (organization_id = current_setting('app.current_organization_id', true)::uuid);
  END IF;
END $$;
