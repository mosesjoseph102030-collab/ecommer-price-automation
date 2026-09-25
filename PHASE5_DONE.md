# Phase 5 complete — waiting for approval

## Included

- Deterministic recommendation engine with hold / raise / lower / investigate / pause states
- Explanation engine covering cost, margin, competitor, stock, rule, ceiling, guardrail, and lock reasons
- Recommendation inbox with urgency, margin-risk, state, category, and product filters
- Approval workflow covering draft, pending, approved, rejected, expired, published, failed, and rolled back
- Per-role approval limits seeded per tenant (store owner 30%, pricing manager 10% by default)
- Pre-queue guardrails: floor, ceiling, staleness, role change limit, and one active execution per request
- WooCommerce publisher that re-reads live product state before writing
- Sale-price guard: a regular-price-only publish never silently changes a customer-visible sale price
- Manual-change conflict detection: unexpected live price becomes a conflict, never an overwrite
- Independent read-back verification after every write
- Safe retry with idempotency key and lost-response reconciliation
- Rollback that restores the prior confirmed price and refuses to clobber later manual changes
- Platform-wide and per-tenant kill switches that cancel queued jobs
- In-app notifications for approval, publish, conflict, failure, and rollback
- Price-change audit with before/after values and source evidence
- Owner `/recommendations` dashboard and admin platform kill switch page
- Phase 5 RLS migration, permissions, role grants, and OpenAPI contracts

## Verification

```text
go vet ./...                       PASS
go test -p 1 ./...                 PASS
go build -p 1 ./...                PASS
OpenAPI YAML parse                 PASS
Docker Compose YAML parse          PASS
migrations/005 SQL paren balance   PASS (0)
```

Test coverage includes 8 recommendation-engine cases, 5 publisher-safety cases
(sale-price guard, manual-change conflict, lost-response reconciliation, idempotent
publish decision, and approval change math), and 2 role-permission tests proving
analysts can never publish and only owners can roll back.

A `-migrate` mode was added to `cmd/api`, and CI now applies every migration twice
against a real PostgreSQL 16 service to prove both correctness and idempotency.

## Known limitations

- Database migrations were not integration-tested against a live PostgreSQL instance
  locally (no Docker/psql and no test credentials in this environment). CI now runs
  `./cmd/api -migrate` twice against PostgreSQL 16, so this is covered on the next
  CI run; run it locally before deploying.
- Next.js production builds and `tsc --noEmit` were not run because `npm install`
  hangs in this environment (previously observed ECONNRESET / ENOTCACHED). The new
  TypeScript and TSX files were balance-checked and reviewed by hand instead.
- Recommendation generation is N+1 per product/variant (one evaluation transaction
  each). Fine at current scale; revisit with a bulk evaluator if catalogs grow large.
- Email and WhatsApp notification channels are queued as schema rows only; the
  provider adapters belong to Phase 6.

## Stop point

No Phase 6 reporting, notifications centre, community, or feedback code was created.
No AI or billing work was started. Awaiting explicit permission before Phase 6.
