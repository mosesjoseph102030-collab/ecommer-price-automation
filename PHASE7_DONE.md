# Phase 7 complete — waiting for approval

## Included

### Governance (the important part)
- `ai_kill_switches` — instant platform-wide and per-tenant kill switch, checked before any other gate
- `ai_feature_flags` — per-tenant, per-feature enablement, default **off**
- `ai_tenant_quotas` — daily request limit, daily token limit, max input bytes
- `ai_invocations` — required audit log: provider, model, prompt-template version, record IDs, input/output tokens, cost, latency, outcome
- `ai_drafts` — the **only** place model output is ever stored; inert until a human approves it

### Capabilities
`recommendation_explanation`, `invoice_ocr`, `rule_assistant`, `product_match_suggestion`,
`pricing_summary`, `feedback_clustering`, `community_summary`, `support_response_draft`.

### Safety implementation
- `DecodeStrict` rejects unknown fields, trailing content, oversized summaries, and non-finite numbers
- `ParseKobo` converts any model-supplied amount to integer kobo with digits-only validation
- `ScreenUntrusted` + `WrapUntrusted` screen and fence OCR, competitor, and community text
- `Redact` / `MinimizeForModel` strip credentials, connection strings, JWTs, and long figures
- `HTTPProvider` is HTTPS-only, blocks private networks, caps the response, and returns a generic error so provider internals never reach a user
- `StaticProvider` keeps the whole governance path testable in CI with no key and no network

## Migrations

| File | Purpose |
|---|---|
| `007_phase7_ai.sql` | AI governance tables, flags, kill switch, quotas, invocations, drafts |
| `008_phase7_ai_write_policies.sql` | Adds the missing tenant write policies on `ai_feature_flags` and `ai_kill_switches` |
| `009_phase7_ai_flag_constraint.sql` | Drops an inverted CHECK constraint on `ai_feature_flags` |

007 is already applied and was **not** edited; 008 and 009 are additive, so no environment drift.

## Live verification against PostgreSQL

The AI path was exercised end to end over HTTP against the real database:

| Check | Result |
|---|---|
| `GET /health/ready` (DB ping) | PASS |
| CORS allows `http://localhost:3001` with credentials | PASS |
| CORS rejects an unknown origin | PASS |
| Protected routes reject anonymous access (401) | PASS |
| Signup issues a JWT and `fintrade_session` cookie | PASS |
| Bearer **and** cookie auth both accepted | PASS |
| Tenant create + scoped read | PASS |
| `GET /reports` and `/reports/kinds` | PASS |
| `POST /ai/generate` returns a **pending** draft + human-gate notice | PASS |
| Raw model output not exposed to the store | PASS |
| Prompt injection refused (`AI_UNTRUSTED_INPUT`) | PASS |
| Owner disables AI for their own store (`AI_KILL_SWITCH`) | PASS |
| Audit log records successes **and** refusals | PASS (4 invocations, incl. `blocked_kill_switch`, `blocked_untrusted_input`) |
| `GET /ai/quota` reports real usage | PASS |

## Bugs found by the live check (all fixed)

1. **Tenant AI controls were completely unusable.** `ai_feature_flags` and `ai_kill_switches` had SELECT-only policies, and those tables are FORCE RLS, so the NOBYPASSRLS application role could read but never write. Every `PUT /ai/features` and `PUT /ai/kill-switch` failed with `42501`. Fixed by migration 008.
2. **An inverted CHECK constraint** on `ai_feature_flags` rejected tenant rows for every feature except `community_summary` — i.e. it permitted exactly the rows nobody writes. Fixed by migration 009.
3. **The AI audit trail wrote zero rows.** `log()` used a bare `ExecContext` without tenant context, so RLS rejected the insert and the error was swallowed. An audit trail that fails silently is worse than none. Fixed to use `db.WithTenant` and surface the failure to the logger.
4. **`GET /ai/quota` always reported 0** because it passed `feature=""` into a query that filtered `WHERE feature=''`. Fixed so an empty feature means "all features".
5. **Owner safety right was mis-gated.** The tenant AI kill switch required the platform-only `PAIConfigure` permission, so a store owner could not disable AI for their own store. Re-gated on `PStoreSettingsUpd`; enabling a feature stays platform-governed.
6. Two Phase 7 core bugs caught by my own tests: `ParseKobo("--1")` was silently reinterpreted as −100 kobo, and `ScreenUntrusted` was bypassed by `"ig nore all previous instructions"`.

## Verification

```text
gofmt / go vet      clean
go test -p 1 ./...  13 packages pass
go build -p 1 ./... OK
```

New tests in this phase cover: strict schema rejection of unknown fields, exact kobo parsing, credential redaction shapes, 13 prompt-injection vectors, provider config validation, cost accounting, static-provider usability without a key, the full feature allowlist, draft payload separation, and Phase 7 permission boundaries (analyst/staff/viewer can never use AI; owners can never configure it).

## Known limitations

- **No real model provider is configured.** `StaticProvider` is active, so governance is fully exercised but responses are canned. Set `AI_PROVIDER_URL`, `AI_PROVIDER_API_KEY`, and `AI_MODEL` to enable a real model.
- **Applying an approved draft is not yet wired to a business-table write.** The approve/reject lifecycle and the `applied` status exist, but each feature still needs its own human-confirmed apply path (e.g. OCR lines → `product_costs` after owner validation). This is deliberate: the spec requires owner validation before save, and each apply is feature-specific.
- Next.js builds and `tsc --noEmit` still cannot run here (`npm install` hangs). No Phase 7 UI page was added for the same reason.
- Community summary is tenant-flagged; there is no platform admin UI for flags or the platform kill switch yet.

## Stop point

No Phase 8 billing, plans, trials, entitlements, usage counters, grace periods, or reliability hardening was created.
Awaiting explicit permission before Phase 8.
