Build this as a multi-tenant WooCommerce pricing intelligence platform with four consistently designed surfaces: public homepage, authentication/onboarding, store-owner dashboard, and platform admin dashboard. The platform should treat tenant isolation, price-publishing safety, auditability, and clear UI as non-negotiable from day one—not as later improvements.

Your visual direction should be clean, calm, practical, and operational: no glassmorphism, no neon/purple-gradient “AI” aesthetic, no excessive animations, and no cluttered dashboards. Use a neutral base, one controlled brand accent, crisp typography, subtle borders, and mobile-first layouts. All text should meet at least the WCAG AA 4.5:1 contrast target for normal text; large text has a 3:1 minimum.

Product definition
Product promise
Help small WooCommerce stores make safer pricing decisions: protect margins, track competitor prices, approve changes confidently, and update WooCommerce without spreadsheets.

Primary users
User	Needs
Store Owner	Understand profitable prices, approve price changes, see store performance, manage team/settings
Pricing Manager	Enter costs, review competitors, create rules, approve changes within limits
Store Analyst	Review reports, recommendations, price history; no publishing access unless granted
Store Staff	Limited catalog/cost data access based on role
Platform Super Admin	Operate the SaaS, manage tenants, plans, themes, security, and escalations
Product Admin	Publish announcements, manage feedback, control feature flags, review product insights
Community Manager	Moderate rooms, manage reports, publish resources, maintain community quality
Support Agent	Assist store owners with connection/sync issues through audited support access
Platform areas
text
Public
/
 /features
 /how-it-works
 /pricing
 /solutions/{industry-slug}
 /security
 /community
 /signin
 /signup

Store owner application
/app/{store-slug}
 /app/{store-slug}/overview
 /app/{store-slug}/products
 /app/{store-slug}/costs
 /app/{store-slug}/competitors
 /app/{store-slug}/rules
 /app/{store-slug}/recommendations
 /app/{store-slug}/price-history
 /app/{store-slug}/reports
 /app/{store-slug}/community
 /app/{store-slug}/settings

Platform administration
/admin
 /admin/stores
 /admin/users
 /admin/subscriptions
 /admin/themes
 /admin/announcements
 /admin/feedback
 /admin/community
 /admin/moderation
 /admin/integrations
 /admin/feature-flags
 /admin/audit-logs
 /admin/system-health
Slug strategy
Use:

text
/app/{store-slug}
Examples:

text
/app/bright-tech-accessories
/app/aboki-home-store
/app/solar-pro-offa
Rules:

Generate from store name.

Lowercase letters, numbers, hyphens only.

Use a UUIDv7/ULID internally; never depend on a slug as the primary database key.

Reserve words such as admin, api, app, login, signup, pricing, support, community.

Keep old-slug redirects when a store changes its name.

Scope every store route using the resolved store/tenant identifier, never a browser-provided tenant ID alone.

UI and design system
Design principles
The product should look like trusted business software—not a landing-page experiment.

Prioritize readability, hierarchy, and task completion.

Use white or warm off-white backgrounds.

Use light gray borders instead of floating glass panels.

Use one primary brand colour and restrained semantic colours.

Use cards only where grouping helps comprehension.

Use data tables for business records; do not turn everything into oversized cards.

Keep shadows subtle and rare.

Use clean icons as supporting cues, not decoration.

Use meaningful empty states and helpful errors.

Keep motion functional and subtle: 120–200ms transitions for menus, dialogs, accordions, and success states only.

Never use motion that delays work or distracts from data.

Use large enough mobile touch targets.

Do not use gradients in core application UI.

Visual design direction
Recommended identity:

Token area	Direction
Main background	#F8FAFC or warm #FAFAF9
Surface/card	#FFFFFF
Primary text	#172033 or #1E293B
Secondary text	#64748B
Border	#E2E8F0
Default brand	Deep teal or disciplined navy, not purple
Success	Deep green
Warning	Amber
Danger	Clear red
Info	Blue
Radius	8px–12px, not overly rounded
Shadow	One subtle card shadow only
Font	Inter, Manrope, or DM Sans
Data font	Optional tabular-number feature for money/data
Recommended base palette
css
:root {
  --bg-canvas: #F8FAFC;
  --bg-surface: #FFFFFF;
  --bg-subtle: #F1F5F9;

  --text-primary: #172033;
  --text-secondary: #5B6475;
  --text-muted: #8993A4;
  --border-default: #E2E8F0;
  --border-strong: #CBD5E1;

  --brand-primary: #0F766E;
  --brand-primary-hover: #115E59;
  --brand-primary-soft: #CCFBF1;
  --on-brand: #FFFFFF;

  --success: #15803D;
  --success-soft: #DCFCE7;
  --warning: #B45309;
  --warning-soft: #FEF3C7;
  --danger: #B91C1C;
  --danger-soft: #FEE2E2;
  --info: #0369A1;
  --info-soft: #E0F2FE;

  --radius-sm: 8px;
  --radius-md: 12px;
  --radius-lg: 16px;
  --shadow-card: 0 1px 2px rgba(15, 23, 42, 0.05);
}
Use semantic tokens in components:

css
.button-primary {
  color: var(--on-brand);
  background: var(--brand-primary);
}

.status-success {
  color: var(--success);
  background: var(--success-soft);
}
Never hardcode a brand hex value inside a component. Theme changes should alter tokens, not require editing hundreds of components.

Curated themes
Launch with 2–4 carefully validated themes.

Theme	Primary colour	Feeling	Best fit
Teal Ledger	#0F766E	Calm, trustworthy, commerce-focused	Default
Navy Commerce	#1E3A8A	Structured, professional	B2B / electronics
Forest Margin	#166534	Growth, inventory, finance	Retail, solar
Charcoal Gold	Gold accent on charcoal-neutral base	Premium and deliberate	Optional higher-tier style
Do not permit arbitrary user-provided hex colors at launch. It creates poor contrast, inconsistent UI, support burden, and a weak product identity.

Theme scope and control
Surface	Theme control
Public homepage	Platform-admin-selected marketing theme
Signup/login	Same platform authentication theme as homepage
Store dashboard	Store owner chooses from platform-approved themes
Store documents	Store theme accent plus store logo, with print-safe layout
Platform admin dashboard	Fixed internal admin theme; do not inherit each tenant’s theme
Community pages	Platform community theme with clear neutral moderation UI
Accessibility rules
Minimum 4.5:1 contrast for normal text.

Minimum 3:1 contrast for large text.

Do not depend only on colour for status.

Support keyboard navigation.

Include visible focus states.

Use proper labels and error messages on fields.

Make dialogs focus-trapped and escapable.

Use readable font sizes; do not default body text to 12px.

Respect reduced-motion preferences.

Use touch targets around 44px where practical.

Test mobile screen readers and desktop keyboard flow.

WCAG 2.2 addresses accessibility across desktop and mobile devices, including contrast guidance intended to support people with visual, hearing, movement, and cognitive accessibility needs.

Homepage and authentication UI
Homepage background-image system
Use a background image only in the hero section, not as a repeating page background. It must visually stop after the hero/intro section and transition cleanly into a plain, readable surface before the rest of the page and footer.

text
Homepage
├── Header
├── Hero section with background image and overlay
├── Trusted-by / proof strip
├── Core value sections on plain background
├── Product features
├── How it works
├── Testimonials or use cases
├── Pricing
├── FAQ
├── Final CTA
└── Footer on solid background
Hero background rules
Use a high-quality, relevant photo:

Small business owner using a laptop/phone.

Retail shelf/product operation.

Business owner checking sales/pricing data.

Avoid generic office handshake imagery.

Avoid visual clutter behind headline text.

Use a dark, accessible overlay:

css
background:
  linear-gradient(
    90deg,
    rgba(8, 30, 34, 0.88) 0%,
    rgba(8, 30, 34, 0.70) 54%,
    rgba(8, 30, 34, 0.30) 100%
  ),
  url("/images/hero-store-owner.webp") center / cover no-repeat;
Limit hero height:

Mobile: content-driven, usually 540–680px.

Desktop: 620–760px.

Ensure heading and CTA contrast passes accessibility checks.

Do not use the image below the hero.

Transition to a solid background with a clear lower boundary.

Preload/compress image; use responsive image formats such as WebP/AVIF.

Provide a neutral colour fallback if image fails to load.

Homepage wireframe
text
[Header]
Logo | Features | How it works | Pricing | Community | Sign in | Start free

[Hero image section]
Eyebrow: WooCommerce price intelligence for small stores
Headline: Price confidently. Protect every margin.
Description: Monitor competitors, calculate your safe price floor,
and approve WooCommerce price updates from one calm dashboard.
[Start free] [Watch how it works]

[Proof strip]
Built for independent WooCommerce stores
Competitor tracking | Margin protection | Safe price publishing

[Problem / outcome]
Stop guessing your prices
Know your margin before you discount
Update WooCommerce without spreadsheets

[Product workflow]
1. Connect store
2. Add costs and competitor links
3. Review safe recommendations
4. Publish or automate within your limits

[Feature sections]
Cost & margin | Competitors | Rules | Approvals | Reporting

[Pricing]
Free trial | Starter | Growth | Pro

[Final CTA]
Connect your WooCommerce store and see your first recommendations.

[Footer]
Product | Resources | Company | Legal | Status | Contact
Signup and login UI
The signup and login pages should inherit the same typography, colours, header logic, and brand identity as the homepage—but use a quieter layout optimized for completion.

text
Desktop:
Left: concise product value / subtle static visual
Right: authentication form card or clean form panel

Mobile:
Logo
Short heading
Form
Trust/support links
Do not use a full-screen blurred background, floating glass card, or animated gradient.

Signup flow
Create account.

Verify email or phone.

Create organization/store name.

Generate/select slug.

Connect WooCommerce.

Import products.

Add initial cost rules.

Add first competitor URL.

Show first dashboard checklist.

Login page features
Email/phone and password.

Password visibility toggle.

Forgot password.

Optional passkey later.

Clear error state without account enumeration.

“Need help?” support link.

No excessive marketing content.

Preserve return URL after login.

Multi-tenant architecture
Tenant model
Each WooCommerce store/customer organization is a tenant.

text
Platform
└── Organization / Tenant
    ├── Store profile
    ├── WooCommerce connection(s)
    ├── Users and memberships
    ├── Products and variants
    ├── Costs and price floors
    ├── Competitors and observations
    ├── Pricing rules
    ├── Recommendations and approvals
    ├── Reports
    ├── Community participation
    └── Audit records
Tenant data strategy
Start with shared PostgreSQL database and shared tables, where all tenant-owned records include organization_id. Pair this with strict application-layer tenant scoping and PostgreSQL Row-Level Security as defense in depth.

This approach is practical for early-stage SaaS, provided you enforce tenant ownership rigorously, use a least-privileged database role for normal requests, and write negative-path cross-tenant tests. OWASP’s multi-tenant guidance specifically recommends enforceable tenant ownership, RLS coverage for tenant-owned tables, constrained request roles, and explicit tests proving cross-tenant access is denied.

Tenant resolution flow
text
Request:
GET /api/v1/app/bright-tech-accessories/recommendations

1. Authenticate user session.
2. Resolve slug → organization ID.
3. Confirm active membership.
4. Load membership roles/permissions.
5. Set request tenant context.
6. Query only organization-scoped records.
7. Enforce RLS context in database transaction.
8. Return allowed data only.
Tenant tables
Every tenant-owned table must include:

text
id
organization_id
created_at
updated_at
created_by_user_id, where relevant
Examples:

text
stores
store_connections
products
product_variants
product_costs
product_price_snapshots
competitors
competitor_products
competitor_price_observations
pricing_rules
price_recommendations
price_change_requests
price_change_approvals
price_change_executions
audit_events
feedback_items
community_room_memberships
Roles and permissions
Do not use only an is_admin boolean. Use role-based permissions.

Store roles
Role	Main capabilities
Store Owner	Full store control, billing, team, integrations, rules, approvals, exports
Pricing Manager	Costs, competitors, pricing rules, recommendations, limited approvals
Analyst	View products, reports, recommendations, history; no publish
Staff	Restricted tasks configured by owner
Viewer	Read-only selected areas
Platform roles
Role	Main capabilities
Super Admin	Entire platform, security-sensitive operations, tenant lifecycle
Platform Admin	Tenants, plans, themes, approved platform settings
Product Admin	Announcements, feedback, feature flags, product analytics
Community Manager	Rooms, moderation, community content
Support Agent	Private support and tightly audited temporary access
Finance Admin	Plans, invoices, subscription reconciliation
Example permissions
text
store.view
store.settings.update
store.integration.connect

product.view
product.cost.update
product.price.publish

competitor.view
competitor.create
competitor.match.review

pricing_rule.create
pricing_rule.update
recommendation.view
recommendation.approve
recommendation.publish
recommendation.rollback

report.view
report.export
team.invite
billing.manage

announcement.read
feedback.create
community.message.create
community.message.moderate

platform.tenant.manage
platform.theme.publish
platform.audit.view
platform.impersonate
Complete build phases
Phase 0 — Research, scope, and design foundation
Outcome: A validated product direction and design standard before code expands.

Build and define
Interview 15–30 WooCommerce store owners.

Select first niche:

Phone accessories/electronics.

Beauty/cosmetics.

Solar components.

Fashion/accessories.

Validate:

Average SKU count.

How often supplier cost changes.

Where they check competitors.

How they currently calculate margin.

Willingness to connect WooCommerce API.

Willingness to approve automated price changes.

Define product KPIs:

Store connection completion rate.

Cost-data completion rate.

Recommendation approval rate.

Successful WooCommerce publish rate.

Margin protected.

Weekly active stores.

Retention.

Produce:

PRD.

Personas.

User journeys.

MVP scope.

Pricing hypothesis.

Security threat model.

Data classification plan.

Design system specification.

Architecture Decision Records.

Exit criteria
Five to ten pilot stores committed.

First pricing workflow tested in a clickable prototype.

Design direction accepted.

MVP scope locked.

Known legal/operational approach for competitor data collection documented.

Phase 1 — Engineering platform and tenant foundation
Outcome: Secure account, tenant, role, slug, audit, theme, and deployment foundations.

Modules
Module	Must include
Authentication	Signup, login, logout, refresh, password reset, verification, session/device management
Organization onboarding	Store name, slug, business category, currency, timezone, onboarding state
Multi-tenancy	organization_id, membership checks, RLS policy, tenant-scoped repositories
Roles/permissions	Store roles, platform roles, permission middleware, UI permission gates
Slug routing	Generate/validate/redirect slugs, reserved-word validation
Audit logging	Sensitive action history, actor, request ID, resource, old/new state where appropriate
Theme system	Curated platform themes, owner selection, admin management, contrast validation
Feature flags	Environment/tenant/user rollout, kill switches, audit logs
File system	Secure file upload, signed URLs, object storage, scanning/quarantine
Notification foundation	In-app notifications, email abstraction, later WhatsApp/push adapters
Platform admin shell	Admin login/MFA, tenant search, audit viewer, settings shell
CI/CD foundation	Docker, environments, secrets, migration process, staging deploy
Database tables
text
users
user_profiles
organizations
organization_settings
organization_onboarding
organization_theme_settings
memberships
roles
permissions
role_permissions
member_role_assignments
sessions
refresh_tokens
verification_tokens
password_reset_tokens
feature_flags
feature_flag_assignments
audit_events
files
file_access_grants
notifications
notification_deliveries
api_idempotency_keys
Exit criteria
User can sign up and create a store organization.

Unique slug is created and routed.

Owner receives correct permissions.

Staff invite works.

Cross-tenant API access fails.

Cross-tenant database access fails in test environment.

Audit events exist for identity, role, theme, and integration-related actions.

Theme token system works across homepage/auth/app shells.

Staging deployment is automated.

Phase 2 — WooCommerce connection and catalog sync
Outcome: A store can safely connect WooCommerce and import its pricing catalog.

WooCommerce recommends REST API v3 for new integrations, using /wp-json/wc/v3/ endpoints; its webhook APIs support programmatic creation and management of webhooks.

Modules
Module	Must include
WooCommerce connector	Credential connection, encrypted storage, connection test, disconnect/reconnect
Initial sync	Products, variants, categories, SKU, price, sale price, stock, tax class, modified timestamps
Webhook ingestion	Verify signature, store raw event securely, deduplicate, queue processing
Reconciliation sync	Scheduled polling to catch missed webhook updates
Catalog normalization	Map WooCommerce product/variation fields into internal models
Sync dashboard	Last sync, errors, queued sync, imported counts, retry/reconnect controls
Conflict detection	Detect manual WooCommerce price changes after platform recommendation/publish
Product status handling	Draft, private, published, deleted, out-of-stock, missing SKU
Import reporting	Completion/error summary, downloadable error report
Connector health	Token/credential health, API errors, webhook status, rate limit state
Required data tables
text
stores
store_connections
store_connection_credentials
store_sync_runs
store_sync_items
webhook_events
webhook_delivery_attempts
products
product_variants
product_categories
product_external_mappings
product_price_snapshots
product_stock_snapshots
Error handling
Invalid URL: show “We could not reach this WooCommerce store.”

Invalid credentials: show safe reconnect prompt; never display secret values.

WooCommerce timeout: retry via queue with exponential backoff.

Rate limit: pause, respect retry window, resume.

Webhook duplicate: deduplicate by provider event ID/hash.

Product deleted upstream: archive mapping; preserve historical data.

Variant changed upstream: flag mapping conflict for review.

Sync partially fails: save completed records, show failed items, permit retry.

Connection revoked: stop publishing, alert owner, require reconnection.

API version unsupported: block connection with actionable compatibility message.

Store server blocks requests: show configuration instructions and support diagnostic ID.

Exit criteria
A connected store imports products and variants accurately.

Manual updates in WooCommerce are recognized.

Webhook + scheduled reconciliation are both functional.

Failed syncs are visible and retryable.

Credentials are encrypted and never returned by API.

A disconnected store cannot receive price-publish jobs.

Phase 3 — Cost, margin, and pricing-rule engine
Outcome: The system produces trustworthy price floors and rules before it attempts automation.

Modules
Module	Must include
Cost manager	Manual per-product cost, bulk CSV import, cost history, effective dates, approval rules
Landed cost	Supplier price, shipping, packaging, payment fees, tax/import cost, other costs
Margin calculator	Gross margin, profit per unit, safe floor, target price
Price floor manager	Product, variant, category, store-level floors; floor precedence
Price ceiling manager	Maximum price, percentage limits, MAP-like policy if needed
Rule builder	Product/category/store rules, activation state, conditions, priority
Product lock	Stop automation/recommendation for sensitive products
Rounding rules	Nearest ₦10, ₦50, ₦100, ending rules, optional
Cost impact alert	“23 products are now below target margin”
Rule simulator	Show expected output for selected products before activation
Price guardrails	Maximum price movement, cooldown, approval thresholds, daily change limits
Core calculations
text
landed_cost =
supplier_cost
+ shipping_cost
+ packaging_cost
+ payment_fee
+ tax_or_import_cost
+ other_allocated_cost
text
gross_margin_percentage =
(selling_price - landed_cost) / selling_price
text
minimum_profitable_price =
landed_cost / (1 - minimum_margin_percentage)
Store monetary values in kobo as integers. Never use float values for money.

Important price rule precedence
text
1. Product manual lock
2. Product explicit floor/ceiling
3. Product-specific rule
4. Variant-specific rule
5. Category rule
6. Store default rule
7. System safety floor
Error handling
Missing cost: no price recommendation; show “Cost required.”

Invalid negative cost: reject.

Currency mismatch: reject or require explicit conversion policy.

Product lacks selling price: mark for review.

Invalid margin ≥100%: reject.

Rule overlap: show precedence preview before activation.

Cost upload duplicates SKU: flag rows for user resolution.

Cost import file invalid: reject with row-level error export.

Price calculation exceeds permitted limit: create alert, not automatic action.

Unrecognized WooCommerce variant: hold for mapping review.

Exit criteria
Every product with valid cost has a calculated floor and margin.

Rules can be previewed before being applied.

No rule can recommend below the configured safe price.

Cost changes generate transparent impact reports.

All cost/rule edits are audited.

Phase 4 — Competitor intelligence and matching
Outcome: The platform monitors approved competitors and produces reliable, reviewable comparison data.

Modules
Module	Must include
Competitor directory	Competitor name, source, location/market, status, monitoring policy
Competitor URL setup	Add URL manually, validate URL, assign to product/variant
Observation worker	Scheduled fetch through approved source/data provider
Price extraction	Price, old price, sale price, currency, availability, timestamp
Evidence	Source URL, captured raw data, optional screenshot, extraction result
Product matching	Suggested, confirmed, rejected, stale, broken states
Match review	Human confirmation screen with side-by-side products/variants
Confidence scoring	Extraction confidence and match confidence
History	Competitor price/availability timeline
Alert engine	Price drop, stock change, stale data, broken URL
Source health	Failure metrics, rate limit, provider outage, job retry queue
Competitor states
text
Unmatched
Suggested
Needs review
Confirmed
Rejected
Stale
Broken
Paused
Safety rules
Competitor observations must have a freshness window.

Low-confidence matches cannot trigger automatic repricing.

Out-of-stock competitor prices must be treated separately.

Promotion/sale price should be labeled distinctly from regular price.

Currency mismatch must not be compared without explicit conversion.

A competitor page/response is untrusted input; never allow it to control rules or prompts.

Use only legally permitted monitoring sources; do not bypass access controls, CAPTCHAs, or protected systems.

Exit criteria
Store owner can add and confirm competitor matches.

History retains observed price and availability.

Broken sources create actionable alerts.

A competitor observation alone cannot publish a price.

All recommendation logic receives match-confidence and freshness information.

Phase 5 — Recommendations, approval workflow, and WooCommerce publishing
Outcome: Store owners receive clear recommendations and safely update WooCommerce.

Modules
Module	Must include
Recommendation engine	Hold, raise, lower, investigate, pause states
Explanation engine	Clear deterministic reasons, cost/margin/competitor/stock impact
Recommendation inbox	Filter by urgency, margin risk, opportunity, category, product
Approval workflow	Draft, pending approval, approved, rejected, expired, published, failed, rolled back
Role limits	Approval thresholds per role and percentage change
WooCommerce publisher	Update regular/sale price, verify response, retry safely
Rollback	Restore prior confirmed price, audit all changes
Conflict handling	Detect external manual price update; do not overwrite blindly
Schedule publishing	Immediate or scheduled time window
Price-change audit	Old/new price, rule, recommendation, approver, source evidence
Kill switch	Disable all automatic publishing platform-wide or per tenant
Notifications	In-app/email/WhatsApp configurable alerts for publish/failure/approval
Recommendation examples
text
Raise price from ₦14,500 to ₦15,000.

Reason:
- Landed cost increased from ₦10,900 to ₦11,400.
- Current margin is 21.4%; your target is 25%.
- Two confirmed competitors are priced at ₦15,200 and ₦15,500.
- You have only 4 units left in stock.

Expected result:
- Estimated gross profit improves by ₦500 per unit.
- New price remains below the lowest confirmed in-stock competitor.
Publishing flow
text
Recommendation created
      ↓
Rules and guardrails validated
      ↓
Owner/authorized manager reviews
      ↓
Approval recorded
      ↓
Publish job queued with idempotency key
      ↓
Worker fetches latest WooCommerce product state
      ↓
Conflict check
      ↓
Update WooCommerce price
      ↓
Verify API response
      ↓
Persist execution + price snapshot + audit event
      ↓
Notify owner
Error handling
Product no longer exists in WooCommerce: mark publish failed; no retry until remapped.

External price changed: pause and ask owner to resolve.

WooCommerce timeout: retry safely with idempotency and verification.

Store credentials expired: stop job and alert owner.

Price below floor: reject before queueing.

Price above ceiling: reject before queueing.

Recommendation stale: expire/recalculate.

Competitor data stale: require recomputation.

Two approvals: enforce one active execution.

Publish succeeded but response was lost: reconcile by fetching product before retrying.

Automatic publishing kill switch: cancel pending jobs safely.

Exit criteria
No price can be published below safe floor.

Every publish has a before/after audit record.

Price updates are idempotent.

Rollback works.

Manual WooCommerce changes are not silently overwritten.

Store owner is notified of publish success/failure.

Phase 6 — Reports, notifications, community, and feedback
Outcome: The platform becomes operationally useful and creates a feedback loop with store owners.

Reporting modules
Product margin report.

Products below floor/target margin.

Competitor gap report.

Price-change history.

Approval/publish success rate.

Competitor monitoring health.

Price recommendation outcome metrics.

Stock-aware pricing opportunities, when stock data is available.

CSV/PDF export.

Scheduled report delivery later.

Permission-controlled report access.

Notification modules
In-app notification centre.

Email notifications.

WhatsApp notifications with consent and rate limits.

Push notifications later.

Read/unread state.

Preferences by alert type.

Critical alerts cannot be silently muted without explicit policy.

Announcements
Admin draft/schedule/publish/archive.

Segment by plan, activity, category, connection status, beta access.

Read and acknowledgement tracking.

Rich content with sanitization.

Audit/version history.

Critical incident notices.

Read-only announcements room.

Feedback
Private bug report/feature request/improvement suggestion.

Screenshot/file upload.

Private thread with product/support team.

Status flow:

text
Submitted → Triaged → Needs information → Planned
→ In progress → Released → Declined → Closed
Duplicate merge.

Feature-voting, optional later.

Feedback analytics and product-insight tags.

Community MVP
Verified owner rooms.

Read-only announcement room.

Room directory.

Posts, replies, reactions.

Search.

Mentions.

Report/mute/block.

Moderation queue.

Community profile with privacy settings.

Anti-spam and rate limits.

No exposure of a store’s private pricing/cost/revenue data.

Community safety requirement
Community discussion may cover general strategy—margins, promotions, bundles, conversion, inventory—but must prohibit coordinated pricing, customer allocation, sharing confidential competitor agreements, or instructions to fix prices across sellers.

Exit criteria
Admin can target and schedule an announcement.

Owner can submit private feedback and receive a response.

Community rooms are moderated.

Private tenant data is not revealed through community profiles/messages.

Reports respect roles and tenant scope.

Notification failures are retryable and visible.

Phase 7 — AI assistance
Outcome: AI reduces work but never becomes an untrusted pricing decision-maker.

AI modules
Feature	Input	Output	User control
Recommendation explanation	Deterministic pricing facts	Clear plain-English summary	Read-only
Supplier invoice OCR	Uploaded document	Draft cost lines	Owner validates before save
Rule assistant	User’s plain-language intention	Proposed pricing rule	Owner reviews/activates
Product-match suggestion	Product metadata and competitor data	Suggested match + confidence	Human confirms
Pricing summary	Store’s aggregated permitted data	Daily/weekly narrative	Read-only
Feedback clustering	Feedback text	Duplicate/topic suggestions	Product team confirms
Community summarization	Public room content	Thread summary	Clearly labeled; moderator controls
Support response draft	Ticket/feedback context	Draft response	Agent reviews before send
AI safety rules
AI does not write directly to production business tables.

AI cannot call WooCommerce publishing endpoints.

AI cannot bypass tenant boundaries.

AI output must conform to a strict schema.

All numerical amounts are recalculated by deterministic Go code.

AI-proposed rules are inactive until a user enables them.

Sensitive customer/store data is minimized/redacted before provider calls.

External model provider credentials remain server-side.

Every AI invocation logs provider, prompt template version, record IDs, cost, and outcome.

AI has per-tenant quotas and rate limits.

AI capabilities must be feature-flagged.

Platform must have instant AI kill switch.

OCR documents and competitor text are untrusted; defend against prompt injection.

Use Go for API control, tenant authorization, queueing, rule validation, and provider integration. Add Python only when advanced forecasting, elasticity modelling, anomaly detection, or custom ML evaluation needs justify it.

Phase 8 — Billing, reliability, hardening, and scale
Outcome: The product is ready for reliable paid usage.

Subscription and billing modules
Plans.

Trial period.

Entitlements.

Usage counters:

Products.

Monitored competitors.

Check frequency.

Team members.

AI usage.

Export limits.

Billing status.

Grace period.

Read-only mode after failed payment, where appropriate.

Invoice/payment history.

Cancellation and retention workflow.

No immediate deletion of business data for nonpayment.

Reliability modules
Health checks.

Queue monitoring.

Dead-letter queues.

Backups.

Restore testing.

Rate limiting.

Circuit breakers for WooCommerce/notification/AI providers.

Graceful degradation.

Incident runbooks.

Status page.

Database performance monitoring.

Tenant usage quotas.

Data-retention jobs.

Disaster recovery drills.

Backend architecture
Recommended services
Start as a modular Go monolith plus independent worker processes.

text
apps/
├── api/                 # Go REST API
├── worker/              # Go background job workers
├── web-public/          # Next.js public website
├── web-app/             # Next.js owner dashboard
├── admin-web/           # Next.js admin dashboard, or one protected app
└── analytics-python/    # Later, optional

internal/
├── auth/
├── organizations/
├── memberships/
├── permissions/
├── audit/
├── stores/
├── woocommerce/
├── catalog/
├── costs/
├── margins/
├── competitors/
├── pricing_rules/
├── recommendations/
├── price_publishing/
├── reports/
├── notifications/
├── announcements/
├── feedback/
├── community/
├── moderation/
├── themes/
├── subscriptions/
├── feature_flags/
├── files/
├── ai/
├── admin/
├── jobs/
└── observability/
API standards
Use /api/v1.

Publish OpenAPI contract.

Generate typed frontend clients.

Return structured errors.

Use cursor pagination.

Use ISO-8601 timestamps in UTC.

Store organization timezone separately.

Use request IDs.

Use idempotency keys for writes that create side effects.

Validate input on backend.

Use database transactions for multi-step writes.

Use explicit allowlists for sort/filter fields.

Implement standard error categories:

text
VALIDATION_ERROR
AUTHENTICATION_REQUIRED
PERMISSION_DENIED
TENANT_ACCESS_DENIED
RESOURCE_NOT_FOUND
CONFLICT
RATE_LIMITED
INTEGRATION_UNAVAILABLE
INTEGRATION_AUTH_FAILED
SYNC_IN_PROGRESS
PRICE_GUARDRAIL_VIOLATION
STALE_RECOMMENDATION
INTERNAL_ERROR
Example error response
json
{
  "error": {
    "code": "PRICE_GUARDRAIL_VIOLATION",
    "message": "This price is below the product's minimum profitable price.",
    "request_id": "req_01JX...",
    "details": {
      "requested_price_kobo": 1050000,
      "minimum_price_kobo": 1200000
    }
  }
}
Database security and schema
Database design principles
PostgreSQL is the source of truth.

Use UUIDv7 or ULID IDs.

Store money as integer kobo.

Store time as timestamptz.

Every tenant-owned table has organization_id.

Use foreign keys and database constraints.

Apply unique constraints scoped by tenant.

Use soft delete/archive states for historical financial and audit records.

Never expose database IDs that can be guessed.

Keep append-only event/audit records for price changes.

Separate production, staging, and development databases.

Do not use production data in development unless anonymized and explicitly approved.

Key tables
text
users
organizations
memberships
roles
permissions
audit_events

stores
store_connections
store_sync_runs
webhook_events

products
product_variants
product_categories
product_costs
product_cost_components
product_price_snapshots
product_stock_snapshots

competitors
competitor_products
competitor_price_observations
competitor_match_reviews

pricing_rules
pricing_rule_versions
product_price_locks
price_recommendations
price_recommendation_reasons
price_change_requests
price_change_approvals
price_change_executions
price_rollbacks

notifications
announcements
feedback_items
community_rooms
community_messages
moderation_actions

subscriptions
plans
entitlements
usage_counters

files
ai_runs
feature_flags
Database constraints
Examples:

text
organizations.slug UNIQUE
memberships(user_id, organization_id) UNIQUE
products(organization_id, external_product_id) UNIQUE
product_variants(organization_id, external_variant_id) UNIQUE
price_change_executions(idempotency_key) UNIQUE
webhook_events(provider_event_id, store_connection_id) UNIQUE
pricing_rules(organization_id, name) UNIQUE where active
Row-level security
For tenant tables:

Enable RLS.

Define tenant policy based on request-scoped tenant setting.

Use a least-privileged application role.

Reset tenant context after each transaction.

Use privileged roles only for migrations/strictly controlled jobs.

Test both allowed and denied paths.

Example:

sql
ALTER TABLE products ENABLE ROW LEVEL SECURITY;
ALTER TABLE products FORCE ROW LEVEL SECURITY;

CREATE POLICY products_tenant_scope
ON products
FOR ALL
USING (
  organization_id = current_setting('app.current_organization_id')::uuid
)
WITH CHECK (
  organization_id = current_setting('app.current_organization_id')::uuid
);
FORCE ROW LEVEL SECURITY can apply RLS to the table owner, but it does not restrict superusers or roles with BYPASSRLS; therefore, the normal application connection must not use those privileged roles.

Encryption and credentials
Encrypt WooCommerce API credentials with envelope encryption/KMS.

Store only encrypted ciphertext, key reference/version, creation/rotation metadata.

Never log credentials.

Never return secret fields from APIs.

Rotate/store credential versions.

Use a secrets manager for environment secrets.

Encrypt database backups.

Use encrypted object storage.

Use TLS for app-to-database and app-to-Redis connections where supported.

Backups and recovery
Daily full backups.

Point-in-time recovery where supported.

Encrypted backup store in separate account/project.

Object storage versioning.

Documented RPO/RTO.

Monthly restore test initially; quarterly minimum once mature.

Restore test verifies:

Tenant isolation.

Product count.

Price history.

Recommendation history.

Audit records.

Object/file links.

Test migration rollback/forward plan in staging.

Frontend modules
Shared design system package
text
Button
IconButton
Input
CurrencyInput
Select
Combobox
Textarea
Checkbox
Radio
Switch
DatePicker
FileUploader
FormField
ValidationMessage
Modal
Drawer
Popover
Tooltip
Dropdown
Tabs
Breadcrumb
Pagination
DataTable
StatusBadge
MetricCard
Chart
EmptyState
ErrorState
LoadingSkeleton
OfflineState
SyncStatus
ConfirmDangerAction
PermissionGate
ThemeProvider
Public website modules
Header/navigation.

Hero with bounded background image.

Product feature sections.

How-it-works flow.

Pricing table.

FAQ.

Footer.

SEO metadata.

Cookie/privacy preference module if required.

Accessibility navigation.

Mobile responsive menus.

Signup/login modules
Auth forms.

Password visibility.

MFA, later.

Recovery flows.

Verification flow.

Onboarding checklist.

Connection wizard.

Error handling.

Auth session redirect.

Owner dashboard modules
Dashboard overview.

Product catalog.

Cost manager.

Competitor matcher.

Rules builder.

Recommendation inbox.

Approval queue.

Price history.

Reports.

Notifications.

Community.

Team/roles.

Integrations.

Theme/settings.

Billing.

Admin dashboard modules
Platform overview.

Tenant/store search.

User management.

Subscription management.

Theme management.

Announcement composer.

Feedback triage.

Community moderation.

Connector diagnostics.

Feature flags.

Audit logs.

System health.

Support access workflow.

Error handling standards
User-facing error principles
Every error should answer:

What happened?

What did not happen?

What can the user do now?

What support reference can they share?

Example:

text
We could not publish this price to WooCommerce.

Your WooCommerce connection timed out, so the product price was not changed.
Try again in a few minutes. If the issue continues, reconnect your store.

Reference: req_01JX...
Error categories and response
Scenario	Product behavior
Validation error	Highlight field, preserve form values, explain expected value
Permission denied	Do not reveal sensitive data; explain access requirement
Network issue	Preserve draft/queue safe action; show retry
WooCommerce timeout	Queue retry; clearly show pending state
Duplicate request	Return original completed result through idempotency
Price conflict	Do not overwrite; show current platform and WooCommerce price side-by-side
Missing cost	No recommendation; direct to cost entry
Stale competitor data	Mark recommendation stale and recheck before publish
AI outage	Keep deterministic features available; show AI unavailable
Worker failure	Move after retries to failed state; alert user/admin appropriately
Database error	Generic safe message + request ID; do not expose SQL details
Attachment rejected	Explain accepted file/size limits; retain no unsafe file
Session expiry	Preserve unsaved safe draft locally if possible; require login and resume
Security measures
Application security
HTTPS everywhere.

HSTS.

Secure cookies.

CSRF protection for cookie-auth flows.

Strict CORS allowlist.

Content Security Policy.

Security headers.

Input validation server-side.

Parameterized SQL.

Output encoding and HTML sanitization.

Rate limits by IP, account, organization, route, and integration.

Brute-force protection.

Request-size limits.

File-upload protections.

Dependency vulnerability scanning.

Secret scanning in repositories and CI.

MFA mandatory for platform admins.

Audit all sensitive actions.

Security event alerts.

Regular patching policy.

Authorization security
Backend permission check on every protected action.

Object-level authorization checks.

Tenant scope applied before resource lookup where possible.

Database RLS as defense in depth.

No client-controlled role/tenant flags.

Short-lived admin/support sessions.

Controlled support impersonation:

Reason required.

Time-bound.

Prominent user-visible banner.

Full audit log.

No password/token visibility.

Immediate session revocation on account suspension or credential compromise.

WooCommerce connector security
Encrypt credentials at rest.

Minimum API permissions.

Use v3 API endpoint for new integrations.

Verify webhook signatures.

Deduplicate events.

Do not allow webhooks to mutate tenant context based only on payload.

Use integration-specific rate limits.

Detect expired/revoked credentials.

Never log provider secrets.

Reconfirm store state before price publishing.

Publish only through approved server-side worker.

Kill switch for automated publishing.

File and OCR security
File MIME and signature validation.

File-size limits.

Malware scanning.

Quarantine suspicious uploads.

Random object keys.

Short-lived signed access URLs.

No public bucket access.

Strip dangerous metadata.

Do not parse unknown office documents unsafely.

OCR processing happens in isolated worker environment.

Treat extracted text as untrusted input.

AI security
AI cannot directly access database or WooCommerce.

Go backend validates permissions and contextual data.

Use strict structured outputs.

Recalculate all money and margin values.

Rate limit AI.

Redact/minimize data.

Audit AI use.

Feature-flag all AI functions.

Detect/contain prompt injection.

Use human confirmation for all impactful actions.

Kill switch on provider compromise/outage.

Testing strategy
Required test layers
Layer	What it protects
Unit tests	Calculations, validation, permissions, rules
Database tests	Constraints, transactions, RLS policies, migrations
API integration tests	Authentication, tenancy, endpoints, connector logic
Contract tests	OpenAPI/client compatibility
Component tests	Forms, tables, dialogs, theme rendering
E2E browser tests	User onboarding and main dashboard workflows
Worker tests	Retries, idempotency, dead-letter paths
Security tests	Authorization, injection, session, upload, webhook attacks
Load tests	Peak sync, recommendation, publish, dashboard traffic
Accessibility tests	Keyboard, labels, focus, contrast
Chaos/recovery tests	Provider failure, queue failure, restore/rollback
AI evaluation tests	Schema, safety, prompt injection, accuracy thresholds
Essential unit tests
Pricing and money
Landed cost calculation.

Margin percentage calculation.

Minimum-price calculation.

Rounding to ₦10/₦50/₦100.

No float arithmetic.

Zero cost behavior.

Negative cost rejected.

Margin of 100% rejected.

Price below floor rejected.

Price above ceiling rejected.

Variant-specific cost/rule precedence.

Product lock overrides all rules.

Rule conflict resolves according to documented priority.

Cooldown prevents repeat change.

Daily maximum price-change count works.

Recommendation is deterministic for same inputs.

Connector and publishing
Credentials are encrypted before persistence.

Invalid credentials rejected safely.

Webhook signature validation.

Duplicate webhook ignored.

Product import preserves external IDs.

Variation import works.

Deleted upstream product is archived.

Publish action is idempotent.

Timeout retry does not duplicate a price change.

External manual price conflict blocks overwrite.

Rollback returns prior known value.

Kill switch stops publish jobs.

Connection revocation stops all publishing.

Tenancy and authorization
User A cannot access User B’s tenant data.

Membership must be active.

Role permission is enforced server-side.

Platform role cannot accidentally become tenant role.

Support access requires granted session/reason.

RLS blocks unscoped cross-tenant query.

Application DB role cannot bypass RLS.

Admin-only endpoints reject non-admin users.

Tenant slug/payload mismatch is rejected.

Essential frontend tests
Homepage hero background ends after hero section.

Homepage/auth/dashboard share token-based color system.

No default purple/gradient styles appear in app UI.

Desktop side navigation collapses into mobile navigation.

Tables become usable mobile lists/cards only where needed.

Form validation is understandable.

Currency input behaves correctly.

Recommendation approval needs confirmation.

Destructive actions need confirmation.

Theme switch does not break contrast/layout.

Empty dashboard points user to connect WooCommerce.

Sync/publish status is clear.

Network failure preserves appropriate drafts.

Error messages expose no secret/technical detail.

Keyboard navigation works.

Screen reader labels exist.

Reduced-motion preference removes nonessential animation.

Essential E2E tests
Store setup
Owner signs up.

Creates organization.

Chooses slug.

Connects WooCommerce.

Imports catalog.

Adds cost.

Defines margin floor.

Adds competitor.

Receives recommendation.

Approves price.

WooCommerce price updates.

Audit history is visible.

Margin-protection flow
Cost increases.

Existing selling price drops below target margin.

Dashboard displays an alert.

User opens recommendation.

User sees calculation and explanation.

User approves.

Publish succeeds.

Price history records old/new price and reason.

Safe conflict flow
Recommendation is generated.

Owner changes price directly inside WooCommerce.

User attempts publish from platform.

Platform detects mismatch.

Platform does not overwrite.

User selects refresh, keep WooCommerce price, or create revised recommendation.

Audit record exists.

Security test cases
IDOR attempts on all tenant resources.

SQL injection in filters/search.

XSS in product/competitor/community content.

CSRF on mutation endpoints.

CORS attack attempt.

Session fixation/reuse.

Refresh token reuse.

Password-reset enumeration.

Brute-force login.

Webhook replay.

Invalid webhook signature.

File upload malware/path traversal.

CSV formula injection.

Prompt injection via competitor page or supplier invoice.

Rate-limit bypass attempt.

Privilege escalation through manipulated JWT/session claims.

Cross-tenant report/export attempt.

Access revoked during active connection.

Secrets absent from logs and API responses.

Edge cases
Tenant and account
Two stores choose the same slug.

Store changes name/slug.

User belongs to multiple stores.

Owner is removed or loses access.

Tenant is suspended with pending jobs.

Staff role changes during active session.

Trial expires mid-workflow.

Store owner deletes connection while jobs are queued.

Store has no products after sync.

Store re-connects with a different WooCommerce domain.

Product and cost
Product has no SKU.

Multiple variants share SKU.

Product has no price.

Product price is zero.

Product is deleted after historical recommendation.

Cost is missing.

Cost changes retroactively.

Product has currency mismatch.

Product is on sale.

Variant is changed/deleted in WooCommerce.

CSV import contains duplicate/malformed SKU.

Selling price is below cost.

Product is manually locked.

Competitor
Competitor URL is broken.

Competitor price is missing.

Competitor page shows a range.

Competitor has a sale price.

Competitor is out of stock.

Competitor currency is different.

Competitor product is not identical variation.

Competitor data is stale.

Competitor source rate-limits requests.

Extracted price looks unreasonable.

Competitor page changes HTML structure.

Publishing
WooCommerce API times out after applying update.

Worker retries same publish.

Owner changes price manually during queued publish.

Price recommendation expires.

Two managers approve simultaneously.

Connector credentials expire.

Store is disconnected.

Price floor increases after recommendation.

Publishing date/timezone is wrong.

Automatic-publish kill switch activates mid-queue.

Rollback fails because WooCommerce product changed.

Sale price and regular price policy conflict.

UI/UX
Small phone screen.

Slow 3G-like connection.

JavaScript fails partly.

User returns after session expiration with unsaved form.

Browser zoom 200%.

Dark mode/system preference changes.

Long business/product names overflow.

Large monetary values.

Right-to-left/language expansion, future readiness.

Empty states.

Long tables.

User has reduced-motion enabled.

Background image fails to load.

Hero image text contrast degrades on device crop.

DevOps, deployment, and operations
Environments
text
local
development
staging
production
disaster-recovery restore environment
CI pipeline
text
Format
Lint
Type check
Go vet/static analysis
Unit tests
Component tests
API contract validation
Database migration tests
Integration tests
Security/dependency scan
Secret scan
Build frontend
Build Go services
Build Docker images
Container scan
Accessibility smoke test
CD pipeline
text
Build immutable artifact
Deploy to staging
Run migrations once via controlled migration job
Run smoke/E2E critical tests
Manual production approval initially
Deploy with rolling/canary release
Run post-deploy health checks
Monitor metrics/errors
Rollback if thresholds fail
Production monitoring
Monitor:

API p95/p99 latency.

Error rate.

Authentication failures.

Database CPU, storage, connections, slow queries.

Queue depth.

Worker retry/dead-letter rate.

WooCommerce sync/publish success.

Webhook failure rate.

Competitor-monitor success.

Recommendation-generation volume.

Price-publish conflict rate.

File scan failures.

AI costs/errors.

Tenant activity and churn signals.

Security incidents and abnormal exports.

Backup and incident runbooks
Create runbooks for:

Database restore.

WooCommerce provider outage.

Redis/queue outage.

Broken deploy rollback.

Credential leak.

Suspicious admin access.

Cross-tenant access incident.

Price publishing incident.

AI provider incident.

Malware upload incident.

Data deletion request.

Notification provider outage.

Final build order
Research and design system

Tenant/auth/roles/slugs/themes/audit foundation

WooCommerce integration and catalog sync

Costs, margin floor, and pricing rules

Competitor tracking and human-reviewed matching

Recommendations, approvals, safe WooCommerce publishing, rollback

Reports, notifications, announcements, feedback, moderated community

AI assistance behind feature flags

Billing, reliability, scale, security hardening