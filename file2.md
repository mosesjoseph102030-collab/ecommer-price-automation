For the WooCommerce price automation SaaS, design two separate but visually consistent products: a focused store-owner dashboard for day-to-day pricing decisions, and a controlled platform-admin console for managing tenants, platform rules, themes, support, announcements, and safety.

The dashboard should not feel like a generic analytics page. Its job is simple: help an owner see what needs attention, understand why a price is recommended, approve changes safely, and prove what changed later. The admin console should help your team run the platform without exposing one store’s sensitive pricing data to another.

Product surfaces
Build four surfaces that use the same design system:

Surface	Main user	Primary job
Public homepage	Prospective store owner	Understand the product, trust it, sign up
Authentication and onboarding	New user	Connect WooCommerce and configure pricing safety
Store-owner dashboard	Merchant / staff member	Review recommendations, approve updates, monitor price health
Platform-admin console	Your internal admin team	Manage tenants, themes, rules, announcements, moderation, support, and platform health
Use a shared visual system across all four:

Same typography, spacing, buttons, form fields, tables, alerts, charts, and empty states.

Different navigation and permissions by role.

No generic glassmorphism.

No purple-gradient “AI SaaS” appearance.

Minimal motion—only small transitions for menus, modals, loading indicators, and success feedback.

Mobile-first layouts, but optimize the main pricing workflow for desktop because merchants will review tables, margins, and comparisons there.

Use a calm, operational theme: clean surfaces, strong contrast, practical tables, readable chart colors, and clear risk states.

Store-owner dashboard
Primary sidebar
The merchant should not see twenty menu items. Keep the main navigation focused on decisions and business outcomes.

text
[Logo / Store switcher]

Overview
Price recommendations
Products
Competitors
Automations
Price history
Reports

Community
Announcements
Feedback & support

Settings
Help center
Profile
Sidebar structure
Menu item	What it solves	Key page content
Overview	“What needs my attention today?”	Action queue, KPIs, recent changes, alerts
Price recommendations	“Which prices should I approve or reject?”	Recommendation table, competitor comparison, margin impact, approve/reject
Products	“Which products are healthy or risky?”	Product catalog, current price, cost, floor, stock, pricing status
Competitors	“Who are we tracking and what changed?”	Competitor URLs, matching status, last seen price, alerts
Automations	“What can change automatically, and under what limits?”	Rules, schedules, approval mode, stop conditions
Price history	“What changed, when, and why?”	Audit log, rollback, outcome notes
Reports	“Is pricing helping sales and margin?”	Trend charts, margin protection, product performance
Community	“What are other store owners discussing?”	Moderated discussions, categories, reporting
Announcements	“What did the platform team tell me?”	Official release notes, maintenance notices, feature guides
Feedback & support	“How do I ask for help or request a feature?”	Private tickets, feedback form, status tracking
Settings	“How is my store connected and protected?”	WooCommerce connection, user roles, notifications, billing, security
Owner overview page
The overview is the merchant’s command center. It should answer four questions within five seconds:

What needs action now?

Are my prices safe?

Did automation change anything recently?

Is the store connection healthy?

Suggested layout:

text
Top bar:
[Store name ▼]   [Date range ▼]   [Notifications]   [Profile]

Page title:
Good morning, BrightTech Store
12 price decisions need your review today.

Primary action:
[Review recommendations]

KPI row:
[Products tracked: 247]
[Needs review: 12]
[Protected margin: ₦184,500]
[Price changes this week: 38]
[Competitor changes: 21]

Main left:
Price action queue
- Product image / product name
- Current price
- Recommended price
- Expected margin
- Competitor range
- Reason
- Approve / Reject / View details

Main right:
Pricing health
- Safe products
- At-risk margin products
- Missing cost data
- Competitor data unavailable

Lower row:
Recent activity                         Competitor movement
- Price updated                         - Competitor X dropped price
- Rule paused                           - Competitor Y out of stock
- WooCommerce sync completed            - 6 products require review

Bottom:
Quick actions
[Add competitor] [Set price rule] [Import costs] [Connect another store]
Key dashboard cards
Card	Meaning	Design treatment
Needs review	Recommendations awaiting human approval	Amber/orange; actionable
Protected by floor	Products prevented from going below margin limit	Green; reassuring
At risk	Missing cost, negative margin, unavailable competitor data, sync problem	Red; must be specific
Automation active	Products/rules currently eligible for auto-update	Neutral blue/teal
Sync health	WooCommerce connection and last sync time	Green if healthy, red if failing
Competitor movement	Major price changes detected	Neutral with trend indicators
Do not use vague cards such as “Revenue Intelligence: 89%” unless users understand the formula. Every number must have a direct explanation and a click-through path.

Price recommendations page
This is the most important page in the product.

text
Price recommendations

[Search products...] [Category ▼] [Risk level ▼] [Status ▼]
[Bulk approve] [Bulk reject] [Export]

Tabs:
All | Needs review | High impact | Auto-applied | Rejected | Expired

Table:
Product | Current | Recommended | Margin | Competitor range | Reason | Risk | Action
Example table:

Product	Current	Recommended	Margin after update	Competitor range	Reason	Action
Logitech M185 Mouse	₦12,000	₦11,700	24%	₦11,500–₦12,100	Main competitor reduced price	Review
USB-C 65W Charger	₦18,500	Keep price	31%	₦17,900–₦20,000	Current price remains competitive	No action
HP 85A Toner	₦22,000	₦23,500	28%	₦23,000–₦25,000	Current price is below market range	Approve
Phone Case	₦4,000	Blocked	8%	₦3,500–₦4,500	New price would breach minimum margin	View reason
When a user clicks a row, open a detail drawer or dedicated product page:

text
Product: Logitech M185 Mouse

Current price: ₦12,000
Recommended price: ₦11,700
Minimum safe price: ₦10,950
Cost price: ₦8,760
Expected margin: 25.1%

Competitor comparison:
Competitor A: ₦11,500, seen 2 hours ago
Competitor B: ₦11,700, seen 4 hours ago
Competitor C: Out of stock

Why we recommend this:
Your price is 4.3% above the lowest in-stock competitor.
The recommended price remains above your configured minimum margin.
This rule is set to “approval required.”

[Approve ₦11,700] [Choose another price] [Reject] [Pause product automation]
The product should always explain the recommendation in plain language. Avoid a black-box label such as “AI says reduce price.”

Products page
This is the full product inventory and price configuration area.

text
Products

[Search] [Category] [Pricing status] [Stock status] [Import / sync]

Product table:
Image | Product | SKU | Cost | Current | Minimum price | Rule | Competitor status | Health
Each product should include:

Product name, SKU, image, category, WooCommerce status.

Selling price.

Supplier cost or landed cost.

Minimum safe price.

Target margin.

Maximum allowed discount or price drop.

Pricing mode: manual, recommendation-only, approval-required, auto-publish.

Linked competitors.

Last competitor price check.

Stock status.

Recent price changes.

Audit history.

The system must clearly flag missing cost data. You cannot safely automate pricing when the margin floor is unknown.

Automations page
This is where the owner creates rules, but rules should be understandable—not like a complicated trading terminal.

text
Automations

[Create rule]

Rule name: Stay competitive on accessories
Applies to: Accessories category
Competitor strategy: Match lowest in-stock competitor
Minimum margin: 20%
Maximum price change: 8%
Frequency: Every 12 hours
Approval mode: Require approval
If competitor data is old: Do not change price
If product stock is low: Do not reduce price
Status: Active
Key pricing modes:

Mode	Behavior	Best for
Manual	Platform provides insight only; owner changes WooCommerce price manually	New or cautious users
Recommendation only	Platform recommends price but never publishes	Testing trust in the product
Approval required	Owner approves each proposed change	Most small stores
Auto-publish within limits	Platform updates only if all safety rules pass	Mature stores with reliable data
Automation must always have guardrails:

Never price below calculated floor.

Never apply a change if cost data is missing.

Never change price when competitor data is stale.

Set maximum price increase/decrease percentage.

Require approval for high-value products.

Pause automation after repeated WooCommerce sync failures.

Allow quick rollback.

Keep immutable audit history.

Price history and rollback
Price history is a trust page, not a boring log page.

text
Price history

Product: HP 85A Toner
Previous: ₦22,000
Changed to: ₦23,500
When: Today, 10:32 AM
Who: Auto rule "Market correction", approved by Chidi
Why: Your current price was below the competitor range
Margin before: 21.3%
Margin after: 28.0%
WooCommerce sync: Successful

[Rollback to ₦22,000]
Every automated or manual change should show:

Old price and new price.

Timestamp.

Actor: owner, staff member, automation rule, or system admin.

Trigger/reason.

Cost and expected margin at the time.

Competitor data used.

Approval status.

WooCommerce publishing result.

Rollback option, where allowed.

Platform-admin dashboard
The platform admin console is not the same as a store-owner dashboard with more buttons. It is an internal operating console. It should have stronger permissions, tenant boundaries, audit trails, and bulk platform controls.

Admin sidebar
text
[Platform logo]

Overview
Tenants
Store connections
Users & roles
Pricing policy controls
Automation jobs
Data sources
Notifications

Content & community
Announcements
Feedback & support
Community moderation

Brand & themes
Theme presets
Homepage content
Feature flags

Platform operations
Audit logs
Security events
System health
Billing & plans

Admin settings
Admin overview
The admin homepage should answer:

Is the platform healthy?

Are background jobs running?

Are WooCommerce syncs failing?

Are any tenants at risk?

Is there a support or moderation backlog?

What requires urgent action?

text
Platform overview

[Active tenants: 148]
[Connected stores: 132]
[Successful sync rate: 98.7%]
[Price updates today: 4,821]
[Failed jobs: 7]
[Open support tickets: 18]
[Flagged community posts: 3]

Critical alerts:
- WooCommerce API failures increasing for 4 tenants
- Competitor source unavailable
- 2 tenants have payment/billing issues
- Background queue latency is above threshold

Operational charts:
- Price recommendations created vs approved
- WooCommerce sync success/failure rate
- Active tenants over time
- Queue processing latency
- Daily API errors
Tenant management
Admins should manage tenants without casually viewing or editing sensitive merchant data.

text
Tenants

[Search by store, domain, email, tenant ID]
[Plan ▼] [Connection status ▼] [Account status ▼]

Store | Plan | WooCommerce status | Products | Last sync | Status | Actions
Tenant detail page:

text
Tenant: BrightTech Store
Tenant ID: tnt_01H...
Plan: Growth
Status: Active

Tabs:
Overview | Connection | Users | Usage | Support | Billing | Audit log | Feature flags

Actions:
[Impersonate with audit record]
[Suspend account]
[Reset WooCommerce connection]
[Disable automation]
[Export tenant data]
[Delete tenant — restricted]
Admin impersonation should be rare and controlled:

Require a reason.

Display a visible “support mode” banner.

Record start and end times.

Log every action.

Avoid showing secrets, passwords, tokens, or full payment details.

Restrict access to senior support roles only.

Pricing safety controls
Admins need platform-level controls, but tenant-specific business decisions should remain with the merchant.

Admin controls can include:

Global minimum and maximum allowed price-change percentage.

Emergency stop for price publishing.

Required margin safeguards.

Disable an unreliable competitor source.

Pause a specific automation engine.

Set stale-data thresholds.

Require approval for newly connected stores.

Restrict auto-publish features to approved plans or tenants.

Do not let a normal admin arbitrarily set a merchant’s price. Admins can enforce platform safety, while merchants own their own pricing strategy.

Jobs and integrations
Because the product depends on background work, this page is essential.

text
Automation jobs

[Status ▼] [Job type ▼] [Tenant ▼] [Time range ▼]

Job ID | Type | Tenant | Started | Duration | Status | Retries | Action
Job types:

WooCommerce product import.

Price synchronization.

Competitor data fetch.

Product matching.

Recommendation calculation.

Price publishing.

Rollback.

Email/WhatsApp notification.

Report generation.

Data cleanup.

Important operational actions:

Retry safe jobs.

View error details.

Pause a queue.

Reprocess a failed import.

Disable a broken competitor connector.

Notify affected merchants.

Escalate repeated failures.

Theme system
The theme system should be centrally governed, not a free color picker. You want 2–4 named themes that maintain accessibility, contrast, and a professional brand across the homepage, authentication pages, user dashboard, and admin console.

Recommended themes
Start with three themes, then introduce a fourth only if it serves a real brand or accessibility need.

Theme	Visual character	Primary use
Operational Blue	Trustworthy, clear, professional	Default platform theme
Commerce Green	Growth, money, healthy pricing	Alternative business-oriented theme
Slate Neutral	Quiet, enterprise, data-focused	Conservative/enterprise customers
Night Ops	Dark interface with high contrast	Optional dark mode, not a separate brand
Theme direction
1. Operational Blue — default
text
Primary: Deep navy / royal blue
Accent: Teal
Success: Green
Warning: Amber
Danger: Red
Background: Soft gray-white
Surface: White
Text: Dark slate
Use it for a strong, practical SaaS identity. This should be the default.

2. Commerce Green
text
Primary: Deep forest green
Accent: Warm gold or muted lime
Success: Emerald
Warning: Amber
Danger: Red
Background: Warm off-white
Surface: White
Text: Charcoal
This works especially well for a pricing, profit, and retail-focused product.

3. Slate Neutral
text
Primary: Slate / charcoal
Accent: Blue
Success: Green
Warning: Amber
Danger: Red
Background: Neutral gray
Surface: White
Text: Near-black
This feels appropriate for agencies, larger merchants, and a more enterprise-oriented platform.

4. Night Ops
text
Primary: Deep navy
Accent: Cyan or muted teal
Success: Green
Warning: Gold
Danger: Coral-red
Background: Very dark blue-gray
Surface: Dark slate
Text: Light gray-white
Do not make dark mode “black with neon colors.” Keep it restrained, readable, and data-friendly.

Design token model
Use semantic design tokens instead of putting raw colors directly into components:

css
:root {
  --color-brand-primary: #1D4ED8;
  --color-brand-secondary: #0F766E;

  --color-bg-page: #F7F8FA;
  --color-bg-surface: #FFFFFF;
  --color-bg-subtle: #F1F5F9;

  --color-text-primary: #172033;
  --color-text-secondary: #667085;
  --color-border: #E4E7EC;

  --color-success: #15803D;
  --color-warning: #B45309;
  --color-danger: #B42318;
  --color-info: #2563EB;

  --color-focus-ring: #2563EB;
}
Components should reference meaning:

css
.button-primary {
  background: var(--color-brand-primary);
  color: var(--color-on-brand);
}

.alert-danger {
  background: var(--color-danger-subtle);
  color: var(--color-danger);
}
They should never hardcode #7C3AED or another color inside a random button component. This makes a safe global theme change possible.

What admins can change
Platform admins can safely switch among approved presets:

text
Brand & themes

Active theme: Operational Blue

[Preview] [Apply to staging] [Publish theme]

Theme settings:
- Logo
- Favicon
- Primary theme preset
- Light/dark mode availability
- Homepage hero image
- Homepage CTA label
- Support email
- Footer legal links
Admin changes should affect:

Homepage.

Sign-up and login pages.

Store-owner dashboard.

Admin dashboard.

Transactional email templates where practical.

Public help center.

Admin changes should not allow unrestricted alterations to:

Success, warning, and error colors.

Accessibility contrast rules.

Button spacing and form behavior.

Role permissions.

System alert styles.

Security pages.

That prevents someone from accidentally making danger alerts look like success messages or creating unreadable color combinations.

Homepage visual direction
Use one full-width but subtle background image or illustration in the hero section only. It should not continue all the way to the footer.

text
Hero section:
- Background: a subtle e-commerce operations image, retail shelf, merchant at work,
  or abstract pricing/data pattern
- Overlay: dark navy or soft neutral overlay for readable text
- Headline: “Price smarter. Protect every margin.”
- CTA: “Connect your WooCommerce store”
- Secondary CTA: “See how it works”

Below hero:
- Clean white/neutral sections
- Feature grids
- How it works
- Safety guarantees
- Testimonials/case studies
- Pricing
- FAQ
- Calm, consistent footer
Avoid:

Full-page photographic backgrounds.

Gradient overload.

Giant floating glass cards.

Excessive blobs, glowing icons, or animated charts.

Different colors on every homepage section.

A hero that claims “AI-powered” without explaining actual merchant value.

Mobile behavior
The dashboard needs a strong mobile experience because merchants may check alerts on their phones.

On mobile:

Sidebar becomes a slide-out drawer.

Keep a bottom quick-action bar for Overview, Recommendations, Products, and More.

Tables become stacked product cards.

Price approval is one-tap, but destructive actions require confirmation.

Do not place dense multi-column competitor data in a narrow screen; show summary first and expand details.

Keep status, recommendation, margin, and action visible above the fold.

Example mobile recommendation card:

text
Logitech M185 Mouse
Current: ₦12,000
Recommended: ₦11,700
Margin after update: 25.1%
Reason: Competitor A reduced its price

[Review] [Approve]
Most important UX rules
Every automated recommendation must explain why it exists.

Every price action must show margin impact before approval.

Every automated publish must be reversible.

Every risky state must be specific: “WooCommerce token expired” is better than “Sync error.”

Do not overload the owner with analytics; prioritize decisions.

Keep platform administration separate from merchant operations.

Use role-based permissions: owner, manager, pricing staff, viewer, support admin, platform admin, super admin.

Keep announcements, private feedback/support, and community discussions as separate areas—not one chaotic chat feed.

Do not expose one tenant’s products, prices, competitors, or audit history to another tenant.

Make theme changes governed, previewable, auditable, and reversible.

Suggested first version
Build this dashboard MVP first:

Store connection and onboarding.

Overview dashboard.

Products page with cost, current price, floor, and status.

Price recommendation queue.

Product recommendation detail view.

Approve, reject, manual override, and rollback.

Price history/audit log.

Automation rules with approval-required mode.

Basic admin: tenants, store connection status, jobs, and announcements.

One default theme plus dark mode or one alternative preset.

Do not begin with complex reports, public community chat, deep theme customization, or automatic price publishing for every store. First prove the core loop:

Import store products → collect trusted data → calculate a safe recommendation → explain it → merchant approves → publish to WooCommerce → retain an audit trail and rollback path.

Once users trust that loop, the rest of the dashboard becomes genuinely valuable rather than decorative.