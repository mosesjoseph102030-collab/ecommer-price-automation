# Phase 6 complete — waiting for approval

## Included

### Reporting
- Product margin report (price, landed cost, margin, floor, stock)
- Products below floor or target margin
- Competitor gap report against confirmed competitor matches
- Price-change history with request, execution, approver role, and errors
- Approval and publish reliability (execution status counts, approval success rate)
- Competitor monitoring health
- Recommendation outcome metrics and publish conversion rate
- Stock-aware pricing opportunities
- CSV export with spreadsheet formula-injection protection, recorded in `report_exports`
- Report access gated by role; export restricted to store owners and pricing managers
- Margin/floor math reuses the Phase 3 calculator so reports cannot disagree with the engine

### Notification centre
- Read/unread state and unread counts
- Per-channel delivery status shown on each notification
- Per-alert-type preferences (in-app, email, WhatsApp)
- Critical alerts (`price_change.conflict`, `price_change.failed`, `security.credential_expired`, `announcement.critical`) cannot be muted without an explicit override plus a recorded reason
- Email and WhatsApp consent records with verification timestamps
- WhatsApp requires an E.164 destination
- Delivery worker with quadratic backoff; exhausted retries marked `failed` and exposed to platform admins

### Announcements
- Draft, publish, schedule, and archive lifecycle
- Audience segmentation by plan, business category, connection status, beta access, activity window, catalog size, and team size
- Audience resolved to concrete organizations at publish time (`announcement_targets`)
- Version history and audit trail
- Read and acknowledgement tracking
- Strict HTML sanitizer (allowlist tags, allowlist attributes, unsafe-URL rejection, dangerous-container removal)

### Feedback
- Private owner-to-support threads with attachments (tenant-validated, `clean` files only)
- Governed status flow: submitted, triaged, needs information, planned, in progress, released, declined, closed
- Internal support notes never returned to the owner
- Duplicate merge, product-insight tags

### Community
- Verified-owner rooms, read-only announcement room, room directory
- Posts, replies, reactions, `@handle` mentions, reports, blocks, mutes
- Moderation queue with hide/restore/escalate, and reply-thread hiding
- Content screening that blocks coordinated pricing, customer allocation, competitor confidentiality, cross-seller price fixing, personal data, and abuse — including obfuscated forms
- Anti-spam rate limiting on posts and replies
- Profiles carry no organization reference

## Phase 6 exit criteria

| Criterion | Where |
|---|---|
| Admin can target and schedule an announcement | `announcements` admin page + `POST /api/v1/admin/announcements` + `PublishDue` worker |
| Owner can submit private feedback and receive a response | `/feedback` page + `feedback_tickets`/`feedback_messages` + `AdminReply` |
| Community rooms are moderated | `community.Moderate`, `admin/community` page, `community_reports` queue |
| Private tenant data is not revealed through community profiles/messages | Community response types have no tenant fields; `TestCommunityPayloadsCarryNoTenantData`; redaction in moderation excerpts |
| Reports respect roles and tenant scope | `PReportView`/`PReportExport` gates; every query runs in `db.WithTenant` |
| Notification failures are retryable and visible | Delivery worker backoff + `failed` status + `GET /api/v1/admin/notifications/failed` |

## Verification

```text
gofmt -l ./cmd ./internal    clean
go vet ./...                 clean
go test -p 1 -count=1 ./...  12 packages pass
go build -p 1 ./...          OK
OpenAPI YAML                 parses (73 paths, 18 schemas)
migrations/006 paren balance 0
```

New tests added in this phase:
- Community safety: 11 tests (allowed strategy discussion is not over-blocked; coordination, price fixing, customer allocation, competitor confidentiality, personal data, obfuscation, and empty content are blocked)
- Community privacy: 5 tests (no tenant data in any community payload; moderation excerpts redact figures; mentions are handles, not emails)
- Announcement sanitizer: 4 tests covering script tags, event handlers, javascript/data URLs, iframe/object/form/svg vectors, safe formatting preservation, and safe-link-only policy
- Feedback status flow: 4 tests (documented path allowed; skips/reversals rejected; every stage can reach closed)
- Reporting: 5 tests (CSV formula-injection neutralization, safe values untouched, integer-kobo money, closed report-kind set, CSV header/rows)
- Notifications: 4 tests (critical alert registry, non-critical types, channel dedupe, error truncation)
- Permissions: 1 new Phase 6 boundary test (reports role-gated, export narrower than read, announcement authoring platform-only, moderation platform-only, triage not granted to store owners)

## Security issues found and fixed during self-review

1. **Announcement cross-tenant leak** — the first inbox query matched all published announcements regardless of audience, which would have shown targeted announcements to untargeted stores. Fixed by resolving the audience into `announcement_targets` at publish time and joining on it.
2. **Broken announcement sanitizer** — escaping before parsing meant no tag was ever recognized, so all legitimate formatting was destroyed. Rewrote it as a proper tokenizer that escapes text and rebuilds only allowlisted tags.
3. **Tenant cost leaking into a shared table** — bare figures like `4500` escaped the money redaction and were written to `community_reports`, which has no tenant RLS. Redaction now also strips bare numbers that appear near commercial terms, while leaving ordinary counts intact.
4. **Email-based mentions contradicted the PII block** — switched mentions to `@handle` and added a unique `handle` column.
5. **Pre-existing Phase 1 RLS gap** — `notification_deliveries` had no RLS, so any tenant could read other tenants' delivery rows. Now scoped through the parent notification.
6. **Duplicated permission constant and contradictory test** — removed the duplicate announcement permission and made export deliberately narrower than read access.

## Known limitations

- Database migrations are still not integration-tested locally (no Docker/psql). CI runs `./cmd/api -migrate` twice against PostgreSQL 16, so `006` is covered on the next CI run.
- Next.js production builds and `tsc --noEmit` were not run because `npm install` hangs in this environment. New `.tsx` files were balance-checked and hand-reviewed only.
- **PDF export is not implemented.** The specification lists "CSV/PDF export"; only CSV is built. Scheduled report delivery is explicitly "later" in the specification and is likewise not built.
- Email and WhatsApp adapters are not configured; only an in-app logging adapter ships, and outbound rows are recorded as queued.
- Community search and feature voting are not implemented (search is not in the module list; voting is marked optional later).
- Rate limiting is database-backed and fixed-window. Phase 8 may replace it with a dedicated limiter.
- Announcement bodies are stored sanitized; the raw author HTML is discarded rather than versioned.

## Stop point

No Phase 7 AI code and no Phase 8 billing, plans, entitlements, or usage metering was created.
Awaiting explicit permission before Phase 7.
