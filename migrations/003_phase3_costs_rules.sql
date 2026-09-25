-- Phase 3: costs, margin floors/ceilings, and pricing rules.
-- No recommendations, approvals, WooCommerce publishing, competitors, AI, or billing.

INSERT INTO permissions(id) VALUES
 ('product.cost.update'),('product.price.manage'),('pricing_rule.create'),('pricing_rule.update')
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS product_costs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  variant_id UUID REFERENCES product_variants(id) ON DELETE CASCADE,
  currency CHAR(3) NOT NULL DEFAULT 'NGN',
  supplier_cost_kobo BIGINT NOT NULL DEFAULT 0 CHECK (supplier_cost_kobo >= 0),
  shipping_cost_kobo BIGINT NOT NULL DEFAULT 0 CHECK (shipping_cost_kobo >= 0),
  packaging_cost_kobo BIGINT NOT NULL DEFAULT 0 CHECK (packaging_cost_kobo >= 0),
  payment_fee_kobo BIGINT NOT NULL DEFAULT 0 CHECK (payment_fee_kobo >= 0),
  tax_import_cost_kobo BIGINT NOT NULL DEFAULT 0 CHECK (tax_import_cost_kobo >= 0),
  other_allocated_cost_kobo BIGINT NOT NULL DEFAULT 0 CHECK (other_allocated_cost_kobo >= 0),
  landed_cost_kobo BIGINT NOT NULL DEFAULT 0 CHECK (landed_cost_kobo >= 0),
  effective_from TIMESTAMPTZ NOT NULL DEFAULT now(),
  effective_to TIMESTAMPTZ,
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('draft','active','superseded')),
  version INT NOT NULL DEFAULT 1,
  created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (effective_to IS NULL OR effective_to > effective_from),
  CHECK (supplier_cost_kobo + shipping_cost_kobo + packaging_cost_kobo + payment_fee_kobo + tax_import_cost_kobo + other_allocated_cost_kobo = landed_cost_kobo)
);
CREATE UNIQUE INDEX IF NOT EXISTS product_costs_active_target_idx
  ON product_costs (organization_id, product_id, COALESCE(variant_id, '00000000-0000-0000-0000-000000000000'::uuid))
  WHERE status = 'active';

CREATE TABLE IF NOT EXISTS product_cost_components (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  product_cost_id UUID NOT NULL REFERENCES product_costs(id) ON DELETE CASCADE,
  component_type TEXT NOT NULL CHECK (component_type IN ('supplier','shipping','packaging','payment_fee','tax_import','other')),
  amount_kobo BIGINT NOT NULL CHECK (amount_kobo >= 0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (product_cost_id, component_type)
);

CREATE TABLE IF NOT EXISTS product_price_policies (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  scope_type TEXT NOT NULL CHECK (scope_type IN ('product','variant','category','store')),
  product_id UUID REFERENCES products(id) ON DELETE CASCADE,
  variant_id UUID REFERENCES product_variants(id) ON DELETE CASCADE,
  category_id UUID REFERENCES product_categories(id) ON DELETE CASCADE,
  minimum_price_kobo BIGINT CHECK (minimum_price_kobo >= 0),
  maximum_price_kobo BIGINT CHECK (maximum_price_kobo >= 0),
  rounding_increment_kobo BIGINT NOT NULL DEFAULT 100 CHECK (rounding_increment_kobo > 0),
  created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (maximum_price_kobo IS NULL OR minimum_price_kobo IS NULL OR maximum_price_kobo >= minimum_price_kobo),
  CHECK ((scope_type='product' AND product_id IS NOT NULL AND variant_id IS NULL AND category_id IS NULL)
      OR (scope_type='variant' AND product_id IS NULL AND variant_id IS NOT NULL AND category_id IS NULL)
      OR (scope_type='category' AND product_id IS NULL AND variant_id IS NULL AND category_id IS NOT NULL)
      OR (scope_type='store' AND product_id IS NULL AND variant_id IS NULL AND category_id IS NULL))
);
CREATE UNIQUE INDEX IF NOT EXISTS product_price_policies_scope_idx ON product_price_policies (
  organization_id, scope_type, COALESCE(product_id, '00000000-0000-0000-0000-000000000000'::uuid),
  COALESCE(variant_id, '00000000-0000-0000-0000-000000000000'::uuid), COALESCE(category_id, '00000000-0000-0000-0000-000000000000'::uuid)
);

CREATE TABLE IF NOT EXISTS pricing_rules (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  scope_type TEXT NOT NULL CHECK (scope_type IN ('product','variant','category','store')),
  product_id UUID REFERENCES products(id) ON DELETE CASCADE,
  variant_id UUID REFERENCES product_variants(id) ON DELETE CASCADE,
  category_id UUID REFERENCES product_categories(id) ON DELETE CASCADE,
  minimum_margin_bps INT NOT NULL CHECK (minimum_margin_bps BETWEEN 1 AND 9999),
  maximum_change_bps INT NOT NULL DEFAULT 2000 CHECK (maximum_change_bps BETWEEN 1 AND 10000),
  maximum_price_kobo BIGINT CHECK (maximum_price_kobo >= 0),
  rounding_increment_kobo BIGINT NOT NULL DEFAULT 100 CHECK (rounding_increment_kobo > 0),
  priority INT NOT NULL DEFAULT 100,
  active BOOLEAN NOT NULL DEFAULT FALSE,
  cooldown_minutes INT NOT NULL DEFAULT 0 CHECK (cooldown_minutes >= 0),
  conditions JSONB NOT NULL DEFAULT '{}',
  version INT NOT NULL DEFAULT 1,
  created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK ((scope_type='product' AND product_id IS NOT NULL AND variant_id IS NULL AND category_id IS NULL)
      OR (scope_type='variant' AND product_id IS NULL AND variant_id IS NOT NULL AND category_id IS NULL)
      OR (scope_type='category' AND product_id IS NULL AND variant_id IS NULL AND category_id IS NOT NULL)
      OR (scope_type='store' AND product_id IS NULL AND variant_id IS NULL AND category_id IS NULL))
);
CREATE UNIQUE INDEX IF NOT EXISTS pricing_rules_active_name_idx
  ON pricing_rules (organization_id, lower(name)) WHERE active;

CREATE TABLE IF NOT EXISTS pricing_rule_versions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  pricing_rule_id UUID NOT NULL REFERENCES pricing_rules(id) ON DELETE CASCADE,
  version INT NOT NULL,
  snapshot JSONB NOT NULL,
  changed_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (pricing_rule_id, version)
);

CREATE TABLE IF NOT EXISTS product_price_locks (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  variant_id UUID REFERENCES product_variants(id) ON DELETE CASCADE,
  locked BOOLEAN NOT NULL DEFAULT TRUE,
  reason TEXT NOT NULL DEFAULT '',
  locked_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS product_price_locks_target_idx
  ON product_price_locks (product_id, COALESCE(variant_id, '00000000-0000-0000-0000-000000000000'::uuid));

CREATE TABLE IF NOT EXISTS cost_impact_alerts (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  cost_id UUID REFERENCES product_costs(id) ON DELETE SET NULL,
  alert_type TEXT NOT NULL CHECK (alert_type IN ('below_target_margin','missing_cost','floor_increase','invalid_rule')),
  previous_value_kobo BIGINT,
  current_value_kobo BIGINT,
  message TEXT NOT NULL,
  acknowledged_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE product_costs ENABLE ROW LEVEL SECURITY;
ALTER TABLE product_costs FORCE ROW LEVEL SECURITY;
ALTER TABLE product_cost_components ENABLE ROW LEVEL SECURITY;
ALTER TABLE product_cost_components FORCE ROW LEVEL SECURITY;
ALTER TABLE product_price_policies ENABLE ROW LEVEL SECURITY;
ALTER TABLE product_price_policies FORCE ROW LEVEL SECURITY;
ALTER TABLE pricing_rules ENABLE ROW LEVEL SECURITY;
ALTER TABLE pricing_rules FORCE ROW LEVEL SECURITY;
ALTER TABLE pricing_rule_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE pricing_rule_versions FORCE ROW LEVEL SECURITY;
ALTER TABLE product_price_locks ENABLE ROW LEVEL SECURITY;
ALTER TABLE product_price_locks FORCE ROW LEVEL SECURITY;
ALTER TABLE cost_impact_alerts ENABLE ROW LEVEL SECURITY;
ALTER TABLE cost_impact_alerts FORCE ROW LEVEL SECURITY;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_product_costs') THEN
    CREATE POLICY tenant_scope_product_costs ON product_costs FOR ALL USING (organization_id=current_setting('app.current_organization_id', true)::uuid) WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_cost_components') THEN
    CREATE POLICY tenant_scope_cost_components ON product_cost_components FOR ALL USING (organization_id=current_setting('app.current_organization_id', true)::uuid) WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_price_policies') THEN
    CREATE POLICY tenant_scope_price_policies ON product_price_policies FOR ALL USING (organization_id=current_setting('app.current_organization_id', true)::uuid) WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_pricing_rules') THEN
    CREATE POLICY tenant_scope_pricing_rules ON pricing_rules FOR ALL USING (organization_id=current_setting('app.current_organization_id', true)::uuid) WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_rule_versions') THEN
    CREATE POLICY tenant_scope_rule_versions ON pricing_rule_versions FOR ALL USING (organization_id=current_setting('app.current_organization_id', true)::uuid) WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_price_locks') THEN
    CREATE POLICY tenant_scope_price_locks ON product_price_locks FOR ALL USING (organization_id=current_setting('app.current_organization_id', true)::uuid) WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_cost_alerts') THEN
    CREATE POLICY tenant_scope_cost_alerts ON cost_impact_alerts FOR ALL USING (organization_id=current_setting('app.current_organization_id', true)::uuid) WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid);
  END IF;
END $$;
