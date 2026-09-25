# Phase 1 — Foundation (DONE)

> Phase 2 was later completed. See `PHASE2_DONE.md` for the current project boundary.

Built strictly inside Phase 1 scope. The document records the Phase 1 boundary at that time; Phase 2 is now implemented separately in `PHASE2_DONE.md`.

## What was built
- Go modular monolith: `cmd/api`, `internal/{config,db,auth,slug,tenancy,permissions,audit,themes,flags,files,notifications,httpapi,observability}`
- Auth: signup/login/logout-ready sessions, JWT, password reset + verification token tables, generic login errors (no enumeration)
- Orgs: name -> slug (`/app/{slug}`), reserved words, duplicate-suffix, old-slug redirects table, onboarding state row
- Tenancy: membership check + role load on every slug route, RLS policies on 8 tenant tables, `app.current_organization_id` context
- RBAC: store roles (owner/manager/analyst/staff/viewer) + platform roles, server-side permission middleware, no `is_admin` boolean
- Audit: append-only `audit_events` for register/login/org/theme/invite
- Themes: 3 curated (`teal-ledger`, `navy-commerce`, `forest-margin`), token CSS in `packages/design-tokens`, contrast gate (>=3:1), admin fixed theme
- Flags: `new_onboarding_checklist`, `email_notifications`, `maintenance_mode` kill switch + assignments
- Files: MIME/size validation (png/jpeg/webp/pdf/csv, 10MB), random object keys, quarantine status
- Notifications: in-app table + deliveries, email interface (log sender for now)
- Admin shell: tenant search + audit viewer behind `platform.tenant.manage`
- API: `/api/v1` with OpenAPI, request IDs, Idempotency-Key on org create, structured errors
- DB: `migrations/001_phase1_foundation.sql` (23 Phase 1 tables + RLS), Docker Compose (api/postgres/redis), Dockerfile, CI (fmt/vet/test/build)
- Web: `apps/web-public` (hero bounded 620px + signup), `apps/web-app/app/[slug]` shell, `apps/admin-web` fixed-theme shell

## Verify
```
go test ./...
go build ./...
docker compose up --build
```

## Exit criteria mapped
- Signup + create org: POST /api/v1/auth/signup -> POST /api/v1/orgs
- Unique slug + routing: slug.Generate/Validate + OrgFromSlug + slug_redirects
- Owner perms: OwnerRoles + member_role_assignments
- Staff invite: POST /api/v1/app/{slug}/team/invite
- Cross-tenant API denied: tenancy.Resolve -> 403 TENANT_ACCESS_DENIED
- Cross-tenant DB denied: RLS policies (test with two orgs in staging)
- Audit events: user.registered/login, org.created, theme.updated, team.invited
- Theme tokens: packages/design-tokens/tokens.css + data-theme across 3 shells
- Staging deploy: Dockerfile + compose + CI workflow

STOPPING HERE. Waiting for your explicit permission before Phase 2.
