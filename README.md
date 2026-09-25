# Pricing Intelligence

Multi-tenant WooCommerce catalog and connection platform built with Go, PostgreSQL, Next.js, and Redis.

## Current status: Phase 8 complete

Implemented through **Phase 8 — billing via Paystack, reliability, hardening, scale**. Development is complete; the phase-by-phase record is in `PHASE1_DONE.md` … `PHASE8_DONE.md`.

### Phase 8 capabilities

- Plans with prices in integer kobo, trial windows, and per-plan entitlements: `products`, `competitors`, `check_frequency_minutes`, `team_members`, `ai_requests`, `export_rows`, `publishes`
- New stores are auto-enrolled on the `trial` plan, so entitlements resolve from the first request
- Per-tenant entitlement overrides that win over the plan — the support grant and grandfathering path
- Atomic monthly usage counters; limits are **enforced**, not just displayed (`ai_requests` and `export_rows` are wired end to end)
- Paystack checkout, server-side transaction re-verification, and `x-paystack-signature` HMAC-SHA512 webhook authentication in constant time
- Webhook idempotency on (provider, event id) — a redelivery is a no-op, not a re-grant
- Grace period after a failed payment, then **read-only mode**: business writes stop, reads, exports, and data continue
- Cancellation and reactivation with a recorded retention window. **Nothing is ever deleted for nonpayment or cancellation**
- Payment and invoice history
- Circuit breakers per WooCommerce store host, persisted so a restart does not clear a tripped breaker
- Dead-letter queue for jobs that exhaust their retries, with operator resolution
- Queue-depth monitoring with oldest-job age, so a stuck queue is distinguishable from an idle one
- Deep health with per-dependency criticality: the database is critical, payments and queues only degrade
- Retention job that prunes operational noise and never business data
- Incidents with a public status page that cannot leak internal detail
- Store dashboard **Billing** page: plan, grace and retention dates, all seven entitlements with live usage, plan catalogue, payment history, and cancellation with a required reason

### Phase 8 safety guarantees

- **Nonpayment never deletes business data.** A lapsed tenant keeps its products, costs, rules, and history, and can export them.
- A lapsed tenant can still reach `/billing/*` and both kill switches. Blocking checkout would make the read-only state permanent; blocking the kill switches would take away a tenant's ability to stop a bad automation.
- Read-only is **method-based**: a GET is never blocked, so a lapsed store keeps its visibility.
- `EffectiveReadOnly` fails **closed** — `past_due` with no recorded grace window is immediately read-only, because failing open would let a lapsed tenant keep publishing.
- Cancellation and trial expiry never impose read-only.
- The Paystack secret key is never stored, never returned to a client, and never logged.
- A payment is only granted after server-side verification, the amount matches the plan price, and the reference belongs to the calling tenant.
- `plans` is RLS-enabled and forced, so the application role can read prices but cannot rewrite them. Changing a price requires a migration.
- Circuit breakers only count infrastructure failures; a 404 for a deleted product does not disable a healthy store.
- Amounts are integer kobo end to end. The browser never formats money into a request; the API decides the price from the plan row.

### Phase 7 capabilities

- Eight governed capabilities: recommendation explanation, invoice OCR, rule assistant, product-match suggestion, pricing summary, feedback clustering, community summary, support response draft
- **AI output never lands in a business table.** Every model response becomes an inert `ai_drafts` row that a human must approve, and only deterministic Go code may apply it
- Strict output schema: unknown fields are rejected outright, not ignored
- Deterministic re-verification: model figures are discarded and recomputed as integer kobo via `ParseKobo`
- Provider credentials stay server-side; transport is HTTPS-only with private-network blocking
- Untrusted text (OCR, competitor pages, community posts) is screened for prompt injection and fenced before it reaches a model
- Store data is minimized and redacted (credentials, connection strings, JWTs, long figures) before every provider call
- Full invocation audit: provider, model, prompt-template version, record IDs, token counts, cost, latency, and outcome — **including refusals**
- Per-tenant daily quotas and per-minute rate limits
- Feature flags per tenant, plus a platform-wide and per-tenant **instant AI kill switch**
- `StaticProvider` fallback so the whole governance path runs in CI and local dev with no provider key and no outbound call
- Phase 7 tenant RLS, permissions, OpenAPI contracts, and three additive migrations

### Phase 7 safety guarantees

- The AI package does not import the WooCommerce package, so it is structurally incapable of publishing a price.
- An active kill switch blocks every call regardless of feature flags.
- Critical refusals (`blocked_kill_switch`, `blocked_untrusted_input`, `blocked_quota`, `schema_rejected`) are written to the audit log, not swallowed.
- Disabling AI is a store-owner safety right and is gated on store settings, not on the platform-only AI configuration permission. Enabling a feature stays platform-governed.
- Raw model output is retained for audit but never returned to store users.

### Phase 6 capabilities

- Eight tenant-scoped reports: product margin, below floor/target margin, competitor gap, price-change history, approval/publish reliability, competitor monitoring health, recommendation outcomes, and stock-aware opportunities
- Report access gated by role; CSV export restricted to store owners and pricing managers
- CSV formula-injection protection on every exported cell
- Notification centre with read/unread state and per-channel delivery status
- Per-alert-type preferences; **critical alerts cannot be muted** without an explicit override reason
- Email and WhatsApp require recorded, verified consent; WhatsApp requires an E.164 destination
- Retryable notification deliveries with backoff; exhausted retries stay visible as `failed`
- Platform announcements with plan, activity, category, connection-status, and beta-access segmentation
- Scheduled publishing, version history, archive, and read/acknowledgement tracking
- Strict HTML sanitization for announcement rich content (allowlist, unsafe-URL and container stripping)
- Private feedback threads with attachments, governed status flow, duplicate merge, and insight tags
- Verified-owner community rooms with posts, replies, reactions, mentions, reports, blocks, and mutes
- Community moderation queue that surfaces auto-blocked content with commercial figures redacted
- Anti-spam rate limiting for community posting and outbound notifications
- Owner pages for reports, alerts, feedback, and community; admin pages for announcements and moderation
- Phase 6 tenant RLS and OpenAPI contracts

### Phase 6 safety guarantees

- **Private tenant data is never exposed through community.** Community response types contain no organization, cost, price, supplier, or revenue field, and moderation excerpts redact commercial figures.
- **Coordinated pricing, customer allocation, competitor confidentiality, and cross-seller price fixing are blocked** before publication, including common obfuscation attempts.
- Announcement targeting is resolved to concrete tenants at publish time, so a targeted announcement cannot leak into another store's inbox.
- Report queries are tenant-scoped through RLS and the tenant context.
- Notification failures are retryable and visible rather than silently dropped.

### Phase 5 capabilities

- Deterministic recommendation engine with `hold`, `raise`, `lower`, `investigate`, and `pause` states
- Explanation engine with cost, margin, competitor, stock, rule, ceiling, guardrail, and lock reasons
- Recommendation inbox with urgency, margin-risk, state, category, and product filters
- Approval workflow: draft, pending, approved, rejected, expired, published, failed, rolled back
- Per-role approval thresholds (store owner and pricing manager), seeded per tenant
- Guardrails enforced before queueing: floor, ceiling, staleness, and role change limit
- WooCommerce publisher that re-reads live state, refuses to overwrite manual changes, and guards active sale prices
- Independent read-back verification after every write, with idempotent retry and lost-response reconciliation
- Rollback that restores the prior confirmed price and refuses to clobber later manual changes
- Platform-wide and per-tenant kill switches that cancel queued jobs safely
- In-app notifications for approval, publish, conflict, failure, and rollback
- Price-change audit with before/after, request, execution, and rollback records
- Owner `/recommendations` dashboard and admin platform kill switch
- Phase 5 tenant RLS and OpenAPI contracts

### Phase 5 safety guarantees

- No price can be published below the minimum profitable price.
- Every publish records a before/after audit event and a price snapshot.
- Publishes are idempotent per request; one active execution per request.
- Manual WooCommerce changes are never silently overwritten (they become conflicts).
- An active sale price blocks a regular-price-only publish instead of changing the customer-visible price.
- Recommendation staleness requires recomputation before it can be approved.

### Phase 4 capabilities

- Administrator-approved competitor domain allowlist
- SSRF-safe HTTPS observation worker with redirect allowlisting and response-size limits
- JSON-LD and Open Graph price/availability extraction with confidence scores
- Immutable observations, SHA-256 evidence, price/availability history, and freshness windows
- Exact-SKU, exact-name and token-similarity product suggestions
- Human confirmation/rejection workflow; matches are never auto-confirmed
- Currency-mismatch, missing-price, price-drop, stock-change, stale-data and broken-source alerts
- Source health, scheduled checks, retry/backoff and platform diagnostics
- Competitor dashboard with source setup, URL tracking, match review and health alerts
- Phase 4 tenant RLS and OpenAPI contracts

### Phase 3 capabilities

- Exact integer-kobo landed-cost calculations with effective cost versions and audit history
- Supplier, shipping, packaging, payment-fee, tax/import, and other-cost components
- Manual and CSV cost entry with row-level duplicate/malformed SKU reporting
- Gross margin, minimum profitable price, price ceilings, and rounding rules
- Product, variant, category, and store pricing policies
- Product/variant/category/store pricing rules with stock/price conditions, version history, and optimistic updates
- Documented rule precedence, product locks, and maximum-price-movement guardrails
- Deterministic rule simulation with no WooCommerce price mutation
- Cost-impact alerts for products below the active target margin
- Owner dashboard pages for `/costs` and `/rules`

### Phase 2 capabilities

- WooCommerce REST API v3 connection test and reconnect
- AES-256-GCM encrypted API credentials; secrets are never returned
- Initial and reconciliation catalog sync worker
- Products, variants, categories, SKU, price, sale price, stock, tax class, and timestamps
- Signed webhook ingress, raw-event storage, and deduplication
- Partial import reports and CSV error reports
- Manual upstream change conflict fields
- Sync retry/backoff, connector health, rate-limit state, and safe errors
- Tenant-scoped PostgreSQL RLS for catalog and connection tables
- Store connection UI, sync history, product table, and admin connector diagnostics

## Local start

Everything runs from the repo root. Three terminals, or `docker compose up --build` for all four.

```powershell
Copy-Item .env.example .env
```

**Backend** — API on `:8080` (also runs migrations on boot):

```powershell
$env:GOMAXPROCS='2'; $env:GOGC='20'   # avoids a Windows Go 1.27.1 linker crash
go run ./cmd/api
```

**Background worker** — competitor checks, recommendations, notifications, dunning, queue monitoring, retention:

```powershell
$env:GOMAXPROCS='2'; $env:GOGC='20'
go run ./cmd/worker
```

**Frontend** — one command per app, from its own directory:

```powershell
cd apps\web-app     ; npm install ; npm run dev     # store dashboard  :3001
cd apps\web-public  ; npm install ; npm run dev     # public marketing  :3000
cd apps\admin-web   ; npm install ; npm run dev     # admin diagnostics :3002
```

Migrations only, without starting the server (what CI runs):

```powershell
go run ./cmd/api -migrate
```

If `tsc` or `next` reports "not recognized" after an install that failed or was interrupted, `node_modules/.bin` is empty:

```powershell
npm rebuild                                            # regenerates the shims
```

If Next reports `next-swc.win32-x64-msvc.node is not a valid Win32 application`, the native binary was truncated; check its size and re-download:

```powershell
(Get-Item node_modules\@next\swc-win32-x64-msvc\next-swc.win32-x64-msvc.node).Length   # ~135 MB
npm cache clean --force
npm install
```

Open:

- Public site: `http://localhost:3000`
- Store dashboard: `http://localhost:3001`
- Admin diagnostics: `http://localhost:3002`
- API health: `http://localhost:8080/health/ready`
- Deep health: `http://localhost:8080/health/details`
- Public status: `http://localhost:8080/api/v1/status`

Create an account and store, then open **Settings → WooCommerce** in the store dashboard. Create WooCommerce REST API keys with the minimum read/write catalog permissions required for synchronization.

## Verify

```powershell
$env:GOMAXPROCS='2'
$env:GOGC='20'
go test -p 1 ./...
go build -p 1 ./...
```

The GOMAXPROCS/GOGC settings avoid a reproducible Windows Go 1.27.1 compiler/linker crash observed on the development machine.

Frontend:

```powershell
cd apps\web-app
npx tsc --noEmit
$env:NODE_OPTIONS='--max-old-space-size=4096'; npm run build
```

`next build` on this machine can exit with a Windows access violation (`3221225477` / `3221226505`) on the first attempt and succeed on a retry. That is the same class of flake as the Go linker crash, not a code fault.

API contract: `openapi/openapi.yaml`

## Database roles

- `pricing_app`: API role, `NOBYPASSRLS`
- `pricing_worker`: controlled worker/admin diagnostics role, `BYPASSRLS`
- `pricing`: migration owner role

Production must provide these through managed secrets rather than the development values in `.env.example`.

Note that `pricing_migrator` owns the tables and is itself subject to `FORCE RLS`, so "ground truth" queries run as the migrator are filtered and report fewer rows than exist. Use `pricing_worker` when counting.

## Payments

Set `PAYSTACK_SECRET_KEY` in `.env` to enable checkout, and point the Paystack dashboard's webhook at `<PUBLIC_BASE>/api/v1/webhooks/paystack`. Leaving the key unset is supported: plans, entitlements, usage metering, and read-only mode all still work, and only checkout is disabled.

```powershell
# .env
PAYSTACK_SECRET_KEY=sk_test_...
BILLING_GRACE_DAYS=7
BILLING_RETENTION_DAYS=90
BILLING_CALLBACK_URL=http://localhost:3001/app
BILLING_DUNNING_INTERVAL_MINUTES=15
```

`BILLING_DUNNING_INTERVAL_MINUTES` matters more than it looks: read-only mode is enforced from stored state, so this worker loop is what actually makes a grace period end. Setting it to `0` would leave lapsed tenants publishing forever.

## Operational endpoints

| Endpoint | Auth | Purpose |
|---|---|---|
| `GET /health/ready` | none | Database ping |
| `GET /health/details` | none | Per-dependency health; `unhealthy` only when the database fails |
| `GET /api/v1/status` | none | Public incident feed, no internal detail |
| `GET /api/v1/status/queues` | none | Queue names and counts only, no tenant identity |
| `GET /api/v1/plans` | session | Public plan catalogue |
| `GET /api/v1/app/{slug}/billing` | `billing.view` | Subscription, entitlements, usage, read-only state |
| `POST /api/v1/webhooks/paystack` | signature | Paystack callback |
| `GET /api/v1/app/{slug}/dead-letters` | `store.view` | This store's failed jobs |
| `GET /api/v1/admin/dead-letters` | platform | All failed jobs |
| `GET /api/v1/admin/queues` | platform | Queue backlog |
| `GET /api/v1/admin/circuit-breakers` | platform | Breaker state |
| `POST /api/v1/admin/circuit-breakers/{name}/reset` | platform | Clear a tripped breaker after a fix |
| `POST /api/v1/admin/incidents` | platform | Open an incident |
| `POST /api/v1/admin/incidents/{id}/resolve` | platform | Resolve an incident |
| `GET /api/v1/admin/retention-runs` | platform | What retention pruned, and when |

## Phase boundary

Development is complete through Phase 8. `PHASE8_DONE.md` records the verification results and the known gaps — most importantly that no live Paystack transaction has been exercised, and that the billing page was verified by build, typecheck, and API contract rather than by rendering it in a browser.
