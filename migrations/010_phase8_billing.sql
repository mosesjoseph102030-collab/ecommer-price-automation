-- Phase 8: billing, entitlements, usage metering, and reliability hardening.
--
-- Payment provider: Paystack. The secret key is read from the environment and
-- never stored in this database, never returned to a client, and never logged.
--
-- Nonpayment never deletes business data. A tenant that stops paying is moved to
-- read-only mode after a grace period; products, costs, rules, and history are
-- retained through a retention window.

INSERT INTO permissions(id) VALUES
 ('billing.view'),('billing.manage')
ON CONFLICT (id) DO NOTHING;

-- ── Plans ──────────────────────────────────────────────────────────────────
-- entitlements is a JSON map of limit keys. Absent key means "use the default
-- in application config"; 0 explicitly means "not allowed on this plan".
CREATE TABLE IF NOT EXISTS plans (
  code TEXT PRIMARY KEY CHECK (code ~ '^[a-z0-9][a-z0-9_-]{1,30}$'),
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  price_kobo BIGINT NOT NULL CHECK (price_kobo >= 0),
  currency CHAR(3) NOT NULL DEFAULT 'NGN',
  interval TEXT NOT NULL DEFAULT 'monthly' CHECK (interval IN ('monthly','annual')),
  trial_days INT NOT NULL DEFAULT 0 CHECK (trial_days >= 0 AND trial_days <= 90),
  -- Paystack plan code used by the subscriptions API. Nullable for free plans.
  paystack_plan_code TEXT NOT NULL DEFAULT '',
  entitlements JSONB NOT NULL DEFAULT '{}',
  is_public BOOLEAN NOT NULL DEFAULT TRUE,
  active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ── Subscriptions ──────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS subscriptions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL UNIQUE REFERENCES organizations(id) ON DELETE CASCADE,
  plan_code TEXT NOT NULL REFERENCES plans(code),
  status TEXT NOT NULL DEFAULT 'trialing' CHECK (status IN ('trialing','active','past_due','cancelled','expired')),
  -- Trial and billing windows.
  trial_ends_at TIMESTAMPTZ,
  current_period_start TIMESTAMPTZ,
  current_period_end TIMESTAMPTZ,
  -- Set when a renewal fails. read_only only after the grace window closes.
  grace_ends_at TIMESTAMPTZ,
  read_only BOOLEAN NOT NULL DEFAULT FALSE,
  read_only_reason TEXT NOT NULL DEFAULT '',
  -- Paystack references. email_token is required by Paystack to charge a
  -- subscription and is treated as a credential: never returned to clients.
  paystack_customer_code TEXT NOT NULL DEFAULT '',
  paystack_subscription_code TEXT NOT NULL DEFAULT '',
  paystack_email_token TEXT NOT NULL DEFAULT '',
  cancel_at_period_end BOOLEAN NOT NULL DEFAULT FALSE,
  cancelled_at TIMESTAMPTZ,
  -- Retention: business data survives cancellation for at least this long.
  retention_ends_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS subscriptions_status_idx ON subscriptions (status, grace_ends_at);

-- Per-tenant overrides of plan entitlements (support grants, grandfathering).
CREATE TABLE IF NOT EXISTS organization_entitlements (
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  entitlement_key TEXT NOT NULL,
  entitlement_value JSONB NOT NULL,
  source TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('plan','manual','support')),
  granted_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (organization_id, entitlement_key)
);

-- ── Usage metering ─────────────────────────────────────────────────────────
-- One row per (org, metric, period). The counter is incremented atomically so
-- concurrent requests cannot lose a unit.
CREATE TABLE IF NOT EXISTS usage_counters (
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  metric TEXT NOT NULL CHECK (metric IN ('products','competitors','check_frequency_minutes','team_members','ai_requests','export_rows','publishes')),
  period_start TIMESTAMPTZ NOT NULL,
  quantity BIGINT NOT NULL DEFAULT 0 CHECK (quantity >= 0),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (organization_id, metric, period_start)
);

-- ── Payments / invoices ────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS payments (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  subscription_id UUID REFERENCES subscriptions(id) ON DELETE SET NULL,
  provider TEXT NOT NULL DEFAULT 'paystack' CHECK (provider IN ('paystack')),
  reference TEXT NOT NULL UNIQUE,
  paystack_invoice_id TEXT NOT NULL DEFAULT '',
  paystack_transaction_id TEXT NOT NULL DEFAULT '',
  plan_code TEXT NOT NULL DEFAULT '',
  amount_kobo BIGINT NOT NULL DEFAULT 0 CHECK (amount_kobo >= 0),
  currency CHAR(3) NOT NULL DEFAULT 'NGN',
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','success','failed','refunded','reversed')),
  channel TEXT NOT NULL DEFAULT '',
  paid_at TIMESTAMPTZ,
  period_start TIMESTAMPTZ,
  period_end TIMESTAMPTZ,
  failure_reason TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS payments_org_idx ON payments (organization_id, created_at DESC);
CREATE INDEX IF NOT EXISTS payments_period_idx ON payments (organization_id, paid_at DESC)
  WHERE status = 'success';

-- Webhook idempotency. Paystack can deliver the same event more than once.
CREATE TABLE IF NOT EXISTS billing_webhook_events (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  provider TEXT NOT NULL DEFAULT 'paystack',
  event TEXT NOT NULL,
  -- Paystack's own event id, used for deduplication.
  provider_event_id TEXT NOT NULL,
  payload_sha256 CHAR(64) NOT NULL,
  processed_at TIMESTAMPTZ,
  outcome TEXT NOT NULL DEFAULT 'received' CHECK (outcome IN ('received','applied','ignored','failed')),
  error TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (provider, provider_event_id)
);

-- ── Reliability ────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS dead_letter_jobs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  queue TEXT NOT NULL,
  job_kind TEXT NOT NULL,
  organization_id UUID REFERENCES organizations(id) ON DELETE SET NULL,
  payload JSONB NOT NULL DEFAULT '{}',
  last_error TEXT NOT NULL DEFAULT '',
  attempts INT NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','replaying','resolved')),
  resolved_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS dead_letter_queue_idx ON dead_letter_jobs (status, queue, created_at);

CREATE TABLE IF NOT EXISTS circuit_breaker_state (
  name TEXT PRIMARY KEY,
  state TEXT NOT NULL DEFAULT 'closed' CHECK (state IN ('closed','open','half_open')),
  consecutive_failures INT NOT NULL DEFAULT 0 CHECK (consecutive_failures >= 0),
  opened_at TIMESTAMPTZ,
  open_until TIMESTAMPTZ,
  last_error TEXT NOT NULL DEFAULT '',
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS incidents (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  severity TEXT NOT NULL CHECK (severity IN ('info','warning','major','critical')),
  status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','monitoring','resolved')),
  title TEXT NOT NULL,
  detail TEXT NOT NULL DEFAULT '',
  public_note TEXT NOT NULL DEFAULT '',
  started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  resolved_at TIMESTAMPTZ,
  created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL
);
CREATE INDEX IF NOT EXISTS incidents_status_idx ON incidents (status, started_at DESC);

CREATE TABLE IF NOT EXISTS retention_runs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  job_name TEXT NOT NULL,
  rows_affected INT NOT NULL DEFAULT 0 CHECK (rows_affected >= 0),
  detail TEXT NOT NULL DEFAULT '',
  started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  finished_at TIMESTAMPTZ
);

-- ── Default plans ──────────────────────────────────────────────────────────
-- Entitlements are read by internal/billing. Values are limits per period.
INSERT INTO plans (code, name, description, price_kobo, currency, interval, trial_days, entitlements) VALUES
 ('trial', 'Trial', 'Full access during the trial period.', 0, 'NGN', 'monthly', 14,
  '{"products":25,"competitors":5,"check_frequency_minutes":1440,"team_members":2,"ai_requests":25,"export_rows":500,"publishes":10}'::jsonb),
 ('starter', 'Starter', 'For a single store getting started.', 1500000, 'NGN', 'monthly', 0,
  '{"products":150,"competitors":15,"check_frequency_minutes":720,"team_members":3,"ai_requests":150,"export_rows":5000,"publishes":150}'::jsonb),
 ('growth', 'Growth', 'For a growing store with active monitoring.', 5000000, 'NGN', 'monthly', 0,
  '{"products":1000,"competitors":50,"check_frequency_minutes":360,"team_members":10,"ai_requests":600,"export_rows":50000,"publishes":1000}'::jsonb),
 ('scale', 'Scale', 'Higher limits and a 60-minute competitor check interval.', 15000000, 'NGN', 'monthly', 0,
  '{"products":10000,"competitors":250,"check_frequency_minutes":60,"team_members":50,"ai_requests":3000,"export_rows":250000,"publishes":10000}'::jsonb)
ON CONFLICT (code) DO NOTHING;

-- ── RLS ────────────────────────────────────────────────────────────────────
ALTER TABLE subscriptions ENABLE ROW LEVEL SECURITY;
ALTER TABLE subscriptions FORCE ROW LEVEL SECURITY;
ALTER TABLE organization_entitlements ENABLE ROW LEVEL SECURITY;
ALTER TABLE organization_entitlements FORCE ROW LEVEL SECURITY;
ALTER TABLE usage_counters ENABLE ROW LEVEL SECURITY;
ALTER TABLE usage_counters FORCE ROW LEVEL SECURITY;
ALTER TABLE payments ENABLE ROW LEVEL SECURITY;
ALTER TABLE payments FORCE ROW LEVEL SECURITY;
ALTER TABLE billing_webhook_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE billing_webhook_events FORCE ROW LEVEL SECURITY;
ALTER TABLE dead_letter_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE dead_letter_jobs FORCE ROW LEVEL SECURITY;
ALTER TABLE circuit_breaker_state ENABLE ROW LEVEL SECURITY;
ALTER TABLE circuit_breaker_state FORCE ROW LEVEL SECURITY;
ALTER TABLE incidents ENABLE ROW LEVEL SECURITY;
ALTER TABLE incidents FORCE ROW LEVEL SECURITY;
ALTER TABLE retention_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE retention_runs FORCE ROW LEVEL SECURITY;

DO $$ BEGIN
  -- Plans are a public catalogue: readable by everyone, writable by nobody at
  -- the application role.
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='plans_read_only') THEN
    CREATE POLICY plans_read_only ON plans FOR SELECT USING (true);
  END IF;

  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_subscriptions') THEN
    CREATE POLICY tenant_scope_subscriptions ON subscriptions FOR ALL
      USING (organization_id=current_setting('app.current_organization_id', true)::uuid)
      WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_entitlements') THEN
    CREATE POLICY tenant_scope_entitlements ON organization_entitlements FOR ALL
      USING (organization_id=current_setting('app.current_organization_id', true)::uuid)
      WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_usage') THEN
    CREATE POLICY tenant_scope_usage ON usage_counters FOR ALL
      USING (organization_id=current_setting('app.current_organization_id', true)::uuid)
      WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_payments') THEN
    CREATE POLICY tenant_scope_payments ON payments FOR ALL
      USING (organization_id=current_setting('app.current_organization_id', true)::uuid)
      WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid);
  END IF;
  -- Webhook events are platform-scoped; written by the controlled admin role.
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='billing_webhooks_read') THEN
    CREATE POLICY billing_webhooks_read ON billing_webhook_events FOR SELECT USING (true);
  END IF;
  -- Dead letters, breakers, and incidents are operational surfaces. Tenants may
  -- read their own; the admin role does the writing.
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='dead_letter_read') THEN
    CREATE POLICY dead_letter_read ON dead_letter_jobs FOR SELECT
      USING (organization_id IS NULL OR organization_id=current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='breaker_read') THEN
    CREATE POLICY breaker_read ON circuit_breaker_state FOR SELECT USING (true);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='incidents_read') THEN
    CREATE POLICY incidents_read ON incidents FOR SELECT USING (true);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='retention_read') THEN
    CREATE POLICY retention_read ON retention_runs FOR SELECT USING (true);
  END IF;
END $$;
