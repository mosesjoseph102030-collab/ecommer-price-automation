# Phase 8 complete — billing, reliability, hardening, scale

Payment provider: **Paystack**. Nonpayment never deletes business data.

## Included

### Billing and entitlements
- `plans` — code, price in kobo, currency, interval, trial days, JSONB entitlements. Seeded with `trial`, `starter`, `growth`, `scale`.
- `subscriptions` — one per org; status `trialing`/`active`/`past_due`/`cancelled`/`expired`, trial window, billing period, grace window, read-only flag, retention deadline.
- `organization_entitlements` — per-tenant overrides. An override always wins over the plan, which is how support grants and grandfathering work.
- `usage_counters` — one row per (org, metric, month), incremented atomically so concurrent requests cannot lose a unit.
- `payments` — invoice/payment history, keyed on a unique `reference`.
- `billing_webhook_events` — webhook idempotency, unique on (provider, provider_event_id).

Entitlement keys: `products`, `competitors`, `check_frequency_minutes`, `team_members`, `ai_requests`, `export_rows`, `publishes`. A limit of `0` means the plan excludes the feature; an absent key means unmetered.

### Nonpayment lifecycle
1. Paystack `subscription.disable` / `subscription.not_renew` → `past_due`, **grace window opens** (7 days default). No deletion.
2. Grace period expires → the dunning worker sets `read_only = true`. This is the only thing that makes read-only actually happen: the request path only *reads* the state.
3. A successful charge clears `past_due`, clears read-only, and upgrades the plan.

Read-only **blocks business writes and keeps reads, exports, and data**. A lapsed tenant is never made read-only for cancelling, and never during a trial.

Two things stay reachable while read-only, deliberately: the `/billing/*` routes (otherwise a lapsed tenant could not pay and the state would be permanent) and both kill switches (a tenant losing the ability to *stop* a bad automation is worse than the automation).

### Paystack integration
- Server-side secret key only. Never stored, never returned to a client, never logged.
- `x-paystack-signature` HMAC-SHA512 verified in constant time over the **raw** body, before any JSON decode.
- Transaction re-verified server-side before a plan is granted. The browser callback is never trusted.
- The charged amount must match the plan price, and the reference must belong to the calling tenant — otherwise a tenant could confirm someone else's payment.
- HTTPS-only transport, private networks blocked outside development.
- The webhook is the source of truth for renewals; duplicate deliveries are deduplicated, not reapplied.

### Reliability
- **Circuit breakers** — persisted state, per store host. A dead WooCommerce store pauses only itself instead of disabling publishing for every tenant. Only infrastructure failures (429, 401/403, 5xx) trip it; a 404 for a deleted product says nothing about reachability.
- **Dead-letter queue** — notification deliveries that exhaust retries are recorded with their payload, error, and attempt count, and are resolvable by an operator.
- **Queue monitoring** — per-queue pending/failed counts plus the age of the oldest job. Depth alone cannot distinguish "idle" from "stuck".
- **Health checks** — `database` is critical (unhealthy); `payments` and `queues` degrade only. A Paystack outage must not take the product offline.
- **Retention job** — prunes operational noise only (webhook attempts, resolved dead letters, processed webhook events). It never touches business data.
- **Incidents + public status page** — internal `detail` and `public_note` are separate fields, so an operator cannot leak internals to the public feed.

## Migrations

| File | Purpose |
|---|---|
| `010_phase8_billing.sql` | Plans, subscriptions, entitlements, usage counters, payments, webhook idempotency, dead letters, breakers, incidents, retention runs |
| `011_phase8_plans_immutable.sql` | Enables + forces RLS on `plans` (see below) |

001–009 were already applied and were **not** edited. Both new files are additive.

## Three bugs this phase found

**1. Plans were mutable by the application role.** Migration 010 created a `SELECT`-only policy on `plans` but never enabled RLS, so the policy was inert and `pricing_app` kept full DML through the default privileges. Any SQL injection in the API could have rewritten a plan's price or entitlements and granted itself unlimited quota. Migration 011 enables and forces RLS. Verified against the real database: `pricing_app` reads 4 plans, `INSERT` is rejected with `new row violates row-level security policy`, and `UPDATE` matches 0 rows. Note that the `UPDATE` returns *success* with 0 rows affected — a bare "no error" assertion would have wrongly passed this.

**2. A lapsed tenant was locked out of paying.** `StartCheckout` called `GuardWrite`, so a store that became read-only for failing to pay could not start a checkout to pay. Read-only would have been permanent. Removed the handler-level guard (the middleware is the single decision point) and added a source-level test that fails if any billing handler reintroduces it.

**3. Duplicate payment references.** `newReference` hashed org + nanosecond timestamp; two checkouts in the same nanosecond produced the same reference, which would have violated the unique constraint on `payments.reference` and broken checkout. Caught by a test that generates references in a tight loop. Now `crypto/rand`.

## Live verification against PostgreSQL and HTTP

| Check | Result |
|---|---|
| Migration applied, then re-run 3× with no change | PASS |
| 98 tables, 82 RLS-enabled, 82 forced, 0 orphan RLS tables | PASS |
| `pricing_app` can read `plans` but not write them | PASS |
| New store auto-enrolled on the `trial` plan with all 7 entitlements | PASS |
| Webhook with no signature header | 401 `INVALID_SIGNATURE` |
| Webhook with a wrong signature | 401 `INVALID_SIGNATURE` |
| Webhook with a tampered body carrying the original signature | 401 `INVALID_SIGNATURE` |
| Webhook correctly signed | 200, plan upgraded trial → starter |
| Same event redelivered | 200, subscription untouched (deduplicated) |
| `subscription.disable` signed webhook | `past_due`, new 7-day grace window, **not** read-only |
| Grace period expired, dunning worker ran | `read_only = true` with reason `payment failed and the grace period has ended` |
| Reads while read-only (store, billing, payments) | 200 |
| Business writes while read-only (rules, competitors, price-policy, sync) | 402 `STORE_READ_ONLY` |
| Checkout while read-only | reaches the provider (502 unreachable) — **not** locked out |
| Kill switches while read-only | 200 |
| Fresh signed `charge.success` | `active`, read-only cleared, upgraded to `growth` |
| Cancel without a reason | 400 `VALIDATION_ERROR` |
| Cancel with a reason | 200, `retention_ends_at` +90 days, **not** read-only, store still readable |
| Reactivate | 200, retention cleared |
| Entitlement override (support grant 3 vs plan 150) | effective limit 3, override wins |
| AI call at the plan limit | 402 `PLAN_LIMIT_EXCEEDED` (not 429), usage not incremented |
| Refused AI calls (kill switch, disabled flag) | usage stays 0 — refusals are free |
| Successful AI calls | `usage.ai_requests` incremented, separate from the daily AI quota |
| Export with `export_rows = 0` | 402 `PLAN_LIMIT_EXCEEDED`, while reads return 200 |
| Platform endpoints as a store owner | 403 `PERMISSION_DENIED` on all five |
| Tenant accessing another store's billing | 403 `TENANT_ACCESS_DENIED` |
| Public status page with a major incident | `degraded`, public note only, no internal detail field |
| Deep health | `database`/`payments`/`queues` all ok |
| Queue backlog | 4 queues reported, none degraded |

`gofmt` clean, `go vet` clean, `go build ./...` OK, `go test -p 1 ./...` — 16 packages pass.

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `PAYSTACK_SECRET_KEY` | unset | Enables checkout. Unset = plans, entitlements, usage, and read-only still work; only checkout is off. |
| `BILLING_GRACE_DAYS` | 7 | Full access after a failed payment. |
| `BILLING_RETENTION_DAYS` | 90 | Retention window recorded on cancellation. |
| `BILLING_CALLBACK_URL` | `http://localhost:3001/app` | Browser return after checkout. |
| `BILLING_DUNNING_INTERVAL_MINUTES` | 15 | How often grace periods and trials expire. |

Set the Paystack dashboard's webhook URL to `<PUBLIC_BASE>/api/v1/webhooks/paystack`.

## Known gaps

- **No real Paystack account was exercised.** The signature path, dedupe, plan changes, dunning, and read-only are all verified against the real database; `initialize transaction` and `verify transaction` were exercised only to the point of an unreachable provider. The first live checkout should be watched.
- **Plan prices are seeded in migration 010 and cannot be changed at runtime** — deliberately, since the application role cannot write `plans`. Changing a price requires a migration. Re-verify the `pricing_app` write check above after any migration that touches `plans`.
- **`products`, `competitors`, `team_members`, and `publishes` are resolvable and enforceable but not yet wired into a mutation path.** `ai_requests` and `export_rows` are enforced end to end. The others need a counting call at each write site and are the natural next increment.
- **No backup or restore drill.** `retention_runs` and the DLQ are in place, but point-in-time recovery has not been tested against a real restore.
- **The billing page was never rendered in a browser.** No desktop browser is attached to this session, so the page was verified at the HTTP and type level: it builds, typechecks clean, and every call it makes returns the expected data with correct CORS. The rendered DOM was not visually confirmed.

## Frontend

The billing UI is `apps/web-app/app/app/[slug]/billing/page.tsx` plus `components/BillingManager.tsx`, with the client methods added to `lib/api.ts`. It shows the current plan and status, the grace-period and retention dates, all seven entitlements with current usage, the plan catalogue, payment history, and cancellation with a required reason. Checkout stashes the Paystack reference in `sessionStorage` before redirecting and re-verifies the transaction server-side on return.

### A claim in the Phase 7 notes was wrong

The Phase 8 work initially recorded that the frontend "cannot be built in this environment". That was **not** correct, and the billing page was left out because of it. The real situation was a corrupt `node_modules` from an earlier interrupted install:

- `node_modules/.bin` was **empty** — packages had been extracted but the executable shims were never written, so `tsc` and `next` were "not found"
- `@next/swc-win32-x64-msvc` was truncated at 15.6 MB and was not a valid Win32 application

`npm rebuild` regenerated the shims, and `npm cache clean --force` plus a re-install replaced the truncated SWC binary with the correct 135 MB one. The registry was reachable throughout; the earlier `ECONNRESET`/`ENOTCACHED` failures were the same interrupted install, not a network block.

Two pre-existing type errors also had to be fixed before the build was clean: `CompetitorManager` and `FeedbackManager` declared their submit handlers as bare `FormEvent`, so `event.currentTarget` was `EventTarget & Element` and could not be passed to `FormData` or `.reset()`. Both are now `FormEvent<HTMLFormElement>`.

### A real frontend/backend bug this uncovered

`Access-Control-Allow-Methods` was `GET, POST, PATCH, OPTIONS` — **`PUT` and `DELETE` were missing**. A browser enforces this by refusing the request, so from the dashboard the Phase 5 pricing kill switch, Phase 6 notification preferences, the price policy, product costs, and rule deletion all silently did nothing, while working perfectly from `curl`. PowerShell-based tests cannot catch this because they do not enforce CORS.

Fixed, with `internal/httpapi/cors_test.go` pinning the verb list, the credential header, the `Vary: Origin` requirement, and the refusal to reflect an unknown origin.

### Verified frontend/backend connection

| Check | Result |
|---|---|
| `GET /app/{slug}/billing` served by the frontend | 200 |
| Preflight from `http://localhost:3001` | 204, `allow-credentials: true`, `Vary: Origin` |
| `GET`/`POST`/`PUT`/`PATCH`/`DELETE` all in `allow-methods` | PASS after the fix |
| Unknown origin reflected back | no |
| `api.plans()` / `billingStatus()` / `billingPayments()` | 200 each, correct `allow-origin` |
| Data the page renders (plan, status, 7 entitlements, trial date) | correct |
| `cancel` with a short reason / with a valid reason | 400 / 200 |
| `reactivate` / `confirm` with no reference | 200 / 400 |
| `npx tsc --noEmit` | 0 errors |
| `npm run build` | succeeded, `/app/[slug]/billing/page` in the route manifest |
