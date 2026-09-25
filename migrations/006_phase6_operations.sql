-- Phase 6: reporting, notification centre, announcements, feedback, and community.
-- No AI (Phase 7) and no billing/plans/entitlements (Phase 8).
-- plan_code and beta_access exist ONLY as announcement segmentation inputs.

INSERT INTO permissions(id) VALUES
 ('report.export'),('notification.manage'),
 ('feedback.triage'),('community.moderate'),('community.post'),
 ('platform.announcement.manage')
ON CONFLICT (id) DO NOTHING;

-- ── Announcement segmentation inputs (not a billing system) ──
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS plan_code TEXT NOT NULL DEFAULT 'beta';
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS beta_access BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS last_active_at TIMESTAMPTZ;

-- ── Reporting ──
CREATE TABLE IF NOT EXISTS report_exports (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  report_kind TEXT NOT NULL CHECK (report_kind IN ('margin','below_floor','competitor_gap','price_change_history','publish_reliability','competitor_health','recommendation_outcomes','stock_opportunity')),
  format TEXT NOT NULL DEFAULT 'csv' CHECK (format IN ('csv')),
  status TEXT NOT NULL DEFAULT 'ready' CHECK (status IN ('pending','ready','failed')),
  row_count INT NOT NULL DEFAULT 0 CHECK (row_count >= 0),
  requested_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS report_exports_org_idx ON report_exports (organization_id, created_at DESC);

-- ── Notification centre ──
CREATE TABLE IF NOT EXISTS notification_consents (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  channel TEXT NOT NULL CHECK (channel IN ('inapp','email','whatsapp')),
  status TEXT NOT NULL DEFAULT 'revoked' CHECK (status IN ('granted','revoked')),
  destination TEXT NOT NULL DEFAULT '',
  verified_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (user_id, channel)
);

-- Critical alert types may never be muted implicitly: muting one requires an
-- explicit override plus a recorded reason.
CREATE TABLE IF NOT EXISTS notification_preferences (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  alert_type TEXT NOT NULL,
  inapp_enabled BOOLEAN NOT NULL DEFAULT TRUE,
  email_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  whatsapp_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  critical_override BOOLEAN NOT NULL DEFAULT FALSE,
  critical_override_reason TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (user_id, alert_type)
);

ALTER TABLE notification_deliveries ADD COLUMN IF NOT EXISTS attempts INT NOT NULL DEFAULT 0;
ALTER TABLE notification_deliveries ADD COLUMN IF NOT EXISTS last_error TEXT NOT NULL DEFAULT '';
ALTER TABLE notification_deliveries ADD COLUMN IF NOT EXISTS next_attempt_at TIMESTAMPTZ;

-- ── Announcements ──
CREATE TABLE IF NOT EXISTS announcements (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  title TEXT NOT NULL,
  body_text TEXT NOT NULL,
  body_html TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','scheduled','published','archived')),
  severity TEXT NOT NULL DEFAULT 'info' CHECK (severity IN ('info','critical')),
  audience JSONB NOT NULL DEFAULT '{}',
  publish_at TIMESTAMPTZ,
  published_at TIMESTAMPTZ,
  current_version INT NOT NULL DEFAULT 1,
  created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS announcements_status_idx ON announcements (status, publish_at);

CREATE TABLE IF NOT EXISTS announcement_versions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  announcement_id UUID NOT NULL REFERENCES announcements(id) ON DELETE CASCADE,
  version INT NOT NULL,
  title TEXT NOT NULL,
  body_text TEXT NOT NULL,
  body_html TEXT NOT NULL DEFAULT '',
  severity TEXT NOT NULL,
  status TEXT NOT NULL,
  changed_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (announcement_id, version)
);

CREATE TABLE IF NOT EXISTS announcement_reads (
  announcement_id UUID NOT NULL REFERENCES announcements(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  read_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  acknowledged_at TIMESTAMPTZ,
  PRIMARY KEY (announcement_id, user_id)
);

-- Audience is resolved to concrete organizations at publish time. Reading the
-- resolved target list is what prevents a targeted announcement from leaking
-- to tenants that were not selected.
CREATE TABLE IF NOT EXISTS announcement_targets (
  announcement_id UUID NOT NULL REFERENCES announcements(id) ON DELETE CASCADE,
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (announcement_id, organization_id)
);
CREATE INDEX IF NOT EXISTS announcement_targets_org_idx ON announcement_targets (organization_id, announcement_id);

-- ── Feedback ──
CREATE TABLE IF NOT EXISTS feedback_tickets (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  kind TEXT NOT NULL CHECK (kind IN ('bug','feature_request','improvement')),
  subject TEXT NOT NULL,
  body TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'submitted' CHECK (status IN ('submitted','triaged','needs_information','planned','in_progress','released','declined','closed')),
  priority TEXT NOT NULL DEFAULT 'normal' CHECK (priority IN ('low','normal','high','urgent')),
  duplicate_of UUID REFERENCES feedback_tickets(id) ON DELETE SET NULL,
  insight_tags TEXT[] NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS feedback_tickets_org_idx ON feedback_tickets (organization_id, created_at DESC);
CREATE INDEX IF NOT EXISTS feedback_tickets_status_idx ON feedback_tickets (status, created_at DESC);

CREATE TABLE IF NOT EXISTS feedback_messages (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  ticket_id UUID NOT NULL REFERENCES feedback_tickets(id) ON DELETE CASCADE,
  author_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  author_kind TEXT NOT NULL CHECK (author_kind IN ('owner','support')),
  body TEXT NOT NULL,
  internal BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS feedback_messages_ticket_idx ON feedback_messages (ticket_id, created_at);

CREATE TABLE IF NOT EXISTS feedback_files (
  ticket_id UUID NOT NULL REFERENCES feedback_tickets(id) ON DELETE CASCADE,
  file_id UUID NOT NULL REFERENCES files(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (ticket_id, file_id)
);

CREATE TABLE IF NOT EXISTS feedback_events (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  ticket_id UUID NOT NULL REFERENCES feedback_tickets(id) ON DELETE CASCADE,
  from_status TEXT NOT NULL DEFAULT '',
  to_status TEXT NOT NULL,
  actor_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS feedback_events_ticket_idx ON feedback_events (ticket_id, created_at);

-- ── Community ──
CREATE TABLE IF NOT EXISTS community_rooms (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  slug TEXT NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL DEFAULT 'owner' CHECK (kind IN ('owner','announcement')),
  requires_verified_owner BOOLEAN NOT NULL DEFAULT TRUE,
  archived BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS community_profiles (
  user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  handle TEXT UNIQUE CHECK (handle IS NULL OR handle ~ '^[a-z0-9][a-z0-9_]{1,30}$'),
  display_name TEXT NOT NULL DEFAULT '',
  headline TEXT NOT NULL DEFAULT '',
  bio TEXT NOT NULL DEFAULT '',
  privacy_level TEXT NOT NULL DEFAULT 'minimal' CHECK (privacy_level IN ('minimal','open')),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS community_memberships (
  room_id UUID NOT NULL REFERENCES community_rooms(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('member','moderator')),
  joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (room_id, user_id)
);

CREATE TABLE IF NOT EXISTS community_posts (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  room_id UUID NOT NULL REFERENCES community_rooms(id) ON DELETE CASCADE,
  author_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  body TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'visible' CHECK (status IN ('visible','hidden','deleted')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS community_posts_room_idx ON community_posts (room_id, created_at DESC);

CREATE TABLE IF NOT EXISTS community_replies (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  post_id UUID NOT NULL REFERENCES community_posts(id) ON DELETE CASCADE,
  author_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  body TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'visible' CHECK (status IN ('visible','hidden','deleted')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS community_replies_post_idx ON community_replies (post_id, created_at);

CREATE TABLE IF NOT EXISTS community_reactions (
  target_type TEXT NOT NULL CHECK (target_type IN ('post','reply')),
  target_id UUID NOT NULL,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('helpful','agree','insight')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (target_type, target_id, user_id, kind)
);

CREATE TABLE IF NOT EXISTS community_mentions (
  target_type TEXT NOT NULL CHECK (target_type IN ('post','reply')),
  target_id UUID NOT NULL,
  mentioned_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (target_type, target_id, mentioned_user_id)
);

CREATE TABLE IF NOT EXISTS community_reports (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  target_type TEXT NOT NULL CHECK (target_type IN ('post','reply')),
  target_id UUID NOT NULL,
  reporter_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  reason TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','reviewing','actioned','dismissed')),
  handled_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  resolution TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS community_reports_status_idx ON community_reports (status, created_at);

CREATE TABLE IF NOT EXISTS community_blocks (
  blocker_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  blocked_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (blocker_user_id, blocked_user_id)
);

CREATE TABLE IF NOT EXISTS community_mutes (
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  room_id UUID NOT NULL REFERENCES community_rooms(id) ON DELETE CASCADE,
  muted_until TIMESTAMPTZ,
  reason TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, room_id)
);

-- ── Shared rate limiting (anti-spam, notification throttling) ──
CREATE TABLE IF NOT EXISTS rate_limit_buckets (
  bucket_key TEXT PRIMARY KEY,
  window_started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  hits INT NOT NULL DEFAULT 0
);

-- Default rooms. Owner rooms require a verified store-owner membership.
--
-- These seeds MUST run before RLS is enabled on community_rooms below. The
-- migration role owns that table, and FORCE ROW LEVEL SECURITY makes even the
-- owner subject to RLS. community_rooms deliberately has no INSERT policy (the
-- application role must never create rooms), so seeding after RLS was armed
-- fails with "new row violates row-level security policy". Seeding first lets
-- the owner insert while it still legitimately owns the table; afterwards the
-- rows remain readable through the SELECT policy, which uses USING (true).
INSERT INTO community_rooms (slug, name, description, kind, requires_verified_owner) VALUES
 ('announcements','Announcements','Read-only platform announcements and incident notices.','announcement',FALSE),
 ('store-owners','Store owners','General strategy, promotions, bundles, conversion, and inventory discussion.','owner',TRUE)
ON CONFLICT (slug) DO NOTHING;

INSERT INTO community_rooms (slug, name, description, kind, requires_verified_owner) VALUES
 ('pricing-strategy','Pricing strategy','Margin, promotion, and bundling strategy. Private tenant pricing data is never discussed here.','owner',TRUE)
ON CONFLICT (slug) DO NOTHING;

-- ── RLS ──
ALTER TABLE report_exports ENABLE ROW LEVEL SECURITY;
ALTER TABLE report_exports FORCE ROW LEVEL SECURITY;
ALTER TABLE notification_consents ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification_consents FORCE ROW LEVEL SECURITY;
ALTER TABLE notification_preferences ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification_preferences FORCE ROW LEVEL SECURITY;
ALTER TABLE announcements ENABLE ROW LEVEL SECURITY;
ALTER TABLE announcements FORCE ROW LEVEL SECURITY;
ALTER TABLE announcement_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE announcement_versions FORCE ROW LEVEL SECURITY;
ALTER TABLE announcement_reads ENABLE ROW LEVEL SECURITY;
ALTER TABLE announcement_reads FORCE ROW LEVEL SECURITY;
ALTER TABLE announcement_targets ENABLE ROW LEVEL SECURITY;
ALTER TABLE announcement_targets FORCE ROW LEVEL SECURITY;
ALTER TABLE feedback_tickets ENABLE ROW LEVEL SECURITY;
ALTER TABLE feedback_tickets FORCE ROW LEVEL SECURITY;
ALTER TABLE feedback_messages ENABLE ROW LEVEL SECURITY;
ALTER TABLE feedback_messages FORCE ROW LEVEL SECURITY;
ALTER TABLE feedback_files ENABLE ROW LEVEL SECURITY;
ALTER TABLE feedback_files FORCE ROW LEVEL SECURITY;
ALTER TABLE feedback_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE feedback_events FORCE ROW LEVEL SECURITY;
ALTER TABLE community_rooms ENABLE ROW LEVEL SECURITY;
ALTER TABLE community_rooms FORCE ROW LEVEL SECURITY;
ALTER TABLE community_profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE community_profiles FORCE ROW LEVEL SECURITY;
ALTER TABLE community_memberships ENABLE ROW LEVEL SECURITY;
ALTER TABLE community_memberships FORCE ROW LEVEL SECURITY;
ALTER TABLE community_posts ENABLE ROW LEVEL SECURITY;
ALTER TABLE community_posts FORCE ROW LEVEL SECURITY;
ALTER TABLE community_replies ENABLE ROW LEVEL SECURITY;
ALTER TABLE community_replies FORCE ROW LEVEL SECURITY;
ALTER TABLE community_reactions ENABLE ROW LEVEL SECURITY;
ALTER TABLE community_reactions FORCE ROW LEVEL SECURITY;
ALTER TABLE community_mentions ENABLE ROW LEVEL SECURITY;
ALTER TABLE community_mentions FORCE ROW LEVEL SECURITY;
ALTER TABLE community_reports ENABLE ROW LEVEL SECURITY;
ALTER TABLE community_reports FORCE ROW LEVEL SECURITY;
ALTER TABLE community_blocks ENABLE ROW LEVEL SECURITY;
ALTER TABLE community_blocks FORCE ROW LEVEL SECURITY;
ALTER TABLE community_mutes ENABLE ROW LEVEL SECURITY;
ALTER TABLE community_mutes FORCE ROW LEVEL SECURITY;
ALTER TABLE rate_limit_buckets ENABLE ROW LEVEL SECURITY;
ALTER TABLE rate_limit_buckets FORCE ROW LEVEL SECURITY;

-- Close a Phase 1 gap: notification_deliveries had no RLS, so any tenant could
-- read another tenant's delivery rows. Scope it through the parent notification.
ALTER TABLE notification_deliveries ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification_deliveries FORCE ROW LEVEL SECURITY;

-- Announcements, rooms, profiles, and the rate-limit table are platform-shared.
-- They deliberately carry no organization_id, so tenant isolation is enforced in
-- Go (read access requires a verified owner) rather than by a tenant predicate.
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_report_exports') THEN
    CREATE POLICY tenant_scope_report_exports ON report_exports FOR ALL
      USING (organization_id=current_setting('app.current_organization_id', true)::uuid)
      WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_notification_consents') THEN
    CREATE POLICY tenant_scope_notification_consents ON notification_consents FOR ALL
      USING (organization_id=current_setting('app.current_organization_id', true)::uuid)
      WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_notification_preferences') THEN
    CREATE POLICY tenant_scope_notification_preferences ON notification_preferences FOR ALL
      USING (organization_id=current_setting('app.current_organization_id', true)::uuid)
      WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_feedback_tickets') THEN
    CREATE POLICY tenant_scope_feedback_tickets ON feedback_tickets FOR ALL
      USING (organization_id=current_setting('app.current_organization_id', true)::uuid)
      WITH CHECK (organization_id=current_setting('app.current_organization_id', true)::uuid);
  END IF;
  -- feedback_messages, feedback_files and feedback_events are child tables with
  -- no organization_id of their own. They are tenant-scoped through their parent
  -- ticket, exactly as notification_deliveries is scoped through notifications
  -- below. Referencing organization_id directly on these tables was a bug.
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_feedback_messages') THEN
    CREATE POLICY tenant_scope_feedback_messages ON feedback_messages FOR ALL
      USING (EXISTS (SELECT 1 FROM feedback_tickets t
                      WHERE t.id=feedback_messages.ticket_id
                        AND t.organization_id=current_setting('app.current_organization_id', true)::uuid))
      WITH CHECK (EXISTS (SELECT 1 FROM feedback_tickets t
                      WHERE t.id=feedback_messages.ticket_id
                        AND t.organization_id=current_setting('app.current_organization_id', true)::uuid));
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_feedback_files') THEN
    CREATE POLICY tenant_scope_feedback_files ON feedback_files FOR ALL
      USING (EXISTS (SELECT 1 FROM feedback_tickets t
                      WHERE t.id=feedback_files.ticket_id
                        AND t.organization_id=current_setting('app.current_organization_id', true)::uuid))
      WITH CHECK (EXISTS (SELECT 1 FROM feedback_tickets t
                      WHERE t.id=feedback_files.ticket_id
                        AND t.organization_id=current_setting('app.current_organization_id', true)::uuid));
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_feedback_events') THEN
    CREATE POLICY tenant_scope_feedback_events ON feedback_events FOR ALL
      USING (EXISTS (SELECT 1 FROM feedback_tickets t
                      WHERE t.id=feedback_events.ticket_id
                        AND t.organization_id=current_setting('app.current_organization_id', true)::uuid))
      WITH CHECK (EXISTS (SELECT 1 FROM feedback_tickets t
                      WHERE t.id=feedback_events.ticket_id
                        AND t.organization_id=current_setting('app.current_organization_id', true)::uuid));
  END IF;
  -- Deliveries are only visible through a notification belonging to this tenant.
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='tenant_scope_notification_deliveries') THEN
    CREATE POLICY tenant_scope_notification_deliveries ON notification_deliveries FOR ALL
      USING (EXISTS (SELECT 1 FROM notifications n
                      WHERE n.id=notification_deliveries.notification_id
                        AND n.organization_id=current_setting('app.current_organization_id', true)::uuid))
      WITH CHECK (EXISTS (SELECT 1 FROM notifications n
                      WHERE n.id=notification_deliveries.notification_id
                        AND n.organization_id=current_setting('app.current_organization_id', true)::uuid));
  END IF;
END $$;

-- Platform-shared tables are readable by any authenticated application role but
-- never writable by it. Writes go through the controlled admin role only.
--
-- Every policy below gets its OWN IF NOT EXISTS guard. Previously all fifteen
-- were nested inside a single guard keyed on 'shared_read_only', so if that one
-- policy already existed the other fourteen were silently skipped while the
-- migration still reported success. That would leave RLS-enabled community
-- tables with no usable policy. Semantics of each policy are unchanged.
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='shared_read_only') THEN
    CREATE POLICY shared_read_only ON announcements FOR SELECT USING (true);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='shared_read_only_v') THEN
    CREATE POLICY shared_read_only_v ON announcement_versions FOR SELECT USING (true);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='shared_read_only_ar') THEN
    CREATE POLICY shared_read_only_ar ON announcement_reads FOR ALL
      USING (user_id=current_setting('app.current_user_id', true)::uuid)
      WITH CHECK (user_id=current_setting('app.current_user_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='shared_read_only_at') THEN
    CREATE POLICY shared_read_only_at ON announcement_targets FOR SELECT USING (true);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='shared_read_only_rooms') THEN
    CREATE POLICY shared_read_only_rooms ON community_rooms FOR SELECT USING (true);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='shared_read_only_profiles') THEN
    CREATE POLICY shared_read_only_profiles ON community_profiles FOR SELECT USING (true);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='shared_read_only_memberships') THEN
    CREATE POLICY shared_read_only_memberships ON community_memberships FOR SELECT USING (true);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='shared_read_only_posts') THEN
    CREATE POLICY shared_read_only_posts ON community_posts FOR SELECT USING (true);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='shared_read_only_replies') THEN
    CREATE POLICY shared_read_only_replies ON community_replies FOR SELECT USING (true);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='shared_read_only_reactions') THEN
    CREATE POLICY shared_read_only_reactions ON community_reactions FOR ALL
      USING (user_id=current_setting('app.current_user_id', true)::uuid)
      WITH CHECK (user_id=current_setting('app.current_user_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='shared_read_only_mentions') THEN
    CREATE POLICY shared_read_only_mentions ON community_mentions FOR SELECT USING (true);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='shared_read_only_reports') THEN
    CREATE POLICY shared_read_only_reports ON community_reports FOR ALL
      USING (reporter_user_id=current_setting('app.current_user_id', true)::uuid)
      WITH CHECK (reporter_user_id=current_setting('app.current_user_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='shared_read_only_blocks') THEN
    CREATE POLICY shared_read_only_blocks ON community_blocks FOR ALL
      USING (blocker_user_id=current_setting('app.current_user_id', true)::uuid)
      WITH CHECK (blocker_user_id=current_setting('app.current_user_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='shared_read_only_mutes') THEN
    CREATE POLICY shared_read_only_mutes ON community_mutes FOR ALL
      USING (user_id=current_setting('app.current_user_id', true)::uuid)
      WITH CHECK (user_id=current_setting('app.current_user_id', true)::uuid);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname='rate_limit_internal') THEN
    CREATE POLICY rate_limit_internal ON rate_limit_buckets FOR ALL USING (true) WITH CHECK (true);
  END IF;
END $$;
