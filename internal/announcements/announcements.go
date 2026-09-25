// Package announcements implements platform-targeted, scheduled, versioned
// announcements with sanitized rich content and read/acknowledgement tracking.
package announcements

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"

	"automation/internal/audit"
	"automation/internal/db"
)

var (
	ErrNotFound = errors.New("announcement not found")
	ErrAudience = errors.New("invalid audience")
)

// Audience selects which organizations receive an announcement. It supports
// segmentation by plan, activity, category, connection status, and beta access,
// exactly as the specification requires. An empty Audience targets everyone.
type Audience struct {
	PlanCodes          []string `json:"plan_codes,omitempty"`
	BusinessCategories []string `json:"business_categories,omitempty"`
	ConnectedOnly      *bool    `json:"connected_only,omitempty"`
	BetaAccess         *bool    `json:"beta_access,omitempty"`
	ActiveWithinDays   *int     `json:"active_within_days,omitempty"`
	OrganizationIDs    []string `json:"organization_ids,omitempty"`
	MinimumProducts    *int     `json:"minimum_products,omitempty"`
	MinimumTeamMembers *int     `json:"minimum_team_members,omitempty"`
}

func (a Audience) Validate() error {
	if len(a.PlanCodes) > 20 || len(a.BusinessCategories) > 20 || len(a.OrganizationIDs) > 500 {
		return ErrAudience
	}
	if a.ActiveWithinDays != nil && (*a.ActiveWithinDays < 0 || *a.ActiveWithinDays > 365) {
		return fmt.Errorf("active_within_days must be 0-365")
	}
	if a.MinimumProducts != nil && (*a.MinimumProducts < 0 || *a.MinimumProducts > 100000) {
		return fmt.Errorf("minimum_products is out of range")
	}
	if a.MinimumTeamMembers != nil && (*a.MinimumTeamMembers < 0 || *a.MinimumTeamMembers > 10000) {
		return fmt.Errorf("minimum_team_members is out of range")
	}
	return nil
}

type Announcement struct {
	ID             string     `json:"id"`
	Title          string     `json:"title"`
	BodyText       string     `json:"body_text"`
	BodyHTML       string     `json:"body_html"`
	Status         string     `json:"status"`
	Severity       string     `json:"severity"`
	Audience       Audience   `json:"audience"`
	PublishAt      *time.Time `json:"publish_at,omitempty"`
	PublishedAt    *time.Time `json:"published_at,omitempty"`
	CurrentVersion int        `json:"current_version"`
	ReadAt         *time.Time `json:"read_at,omitempty"`
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

type Service struct {
	DB    *sql.DB
	Admin *sql.DB
}

var (
	// allowedTags is a strict allowlist. Anything else is stripped, so admin
	// rich content cannot inject scripts, styles, or iframes.
	allowedTags = map[string]bool{
		"b": true, "strong": true, "i": true, "em": true, "u": true, "p": true,
		"br": true, "ul": true, "ol": true, "li": true, "h2": true, "h3": true,
		"code": true, "pre": true, "a": true, "blockquote": true,
	}
	// allowedAttributes limits links to safe, non-scripting forms.
	allowedAttributes = map[string]bool{"href": true, "title": true}
	unsafeURL         = regexp.MustCompile(`(?i)^\s*(javascript:|data:|vbscript:|file:)`)
)

// SanitizeHTML rebuilds markup from an allowlist. Text is escaped and only
// allowlisted tags with allowlisted, safe attributes are re-emitted, so any tag
// or attribute the author did not intend cannot survive.
func SanitizeHTML(input string) string {
	var b strings.Builder
	i := 0
	for i < len(input) {
		lt := strings.IndexByte(input[i:], '<')
		if lt < 0 {
			b.WriteString(html.EscapeString(input[i:]))
			break
		}
		lt += i
		// Text preceding the tag is user content and must be escaped.
		b.WriteString(html.EscapeString(input[i:lt]))

		gt := strings.IndexByte(input[lt:], '>')
		if gt < 0 {
			// Unterminated tag: treat the remainder as text.
			b.WriteString(html.EscapeString(input[lt:]))
			break
		}
		gt += lt
		tagText := input[lt : gt+1]
		name, closing, selfClosing, attrs := parseTag(tagText)
		if name == "" {
			i = gt + 1
			continue
		}
		if !allowedTags[name] {
			// Drop the tag. For containers whose content is itself dangerous
			// (script, style, iframe...), also drop everything up to the close.
			if !closing && dangerousContainers[name] {
				i = skipElement(input, gt+1, name)
				continue
			}
			i = gt + 1
			continue
		}
		if closing {
			b.WriteString("</" + name + ">")
			i = gt + 1
			continue
		}
		b.WriteString("<" + name)
		for _, attr := range attrs {
			key, value := splitAttr(attr)
			if !allowedAttributes[key] {
				continue
			}
			if key == "href" && !safeURL(value) {
				continue
			}
			b.WriteString(" " + key + `="` + html.EscapeString(value) + `"`)
		}
		if selfClosing || voidTags[name] {
			b.WriteString(" />")
		} else {
			b.WriteString(">")
		}
		i = gt + 1
	}
	return b.String()
}

// dangerousContainers must be dropped together with their content.
var dangerousContainers = map[string]bool{
	"script": true, "style": true, "iframe": true, "object": true, "embed": true,
	"form": true, "svg": true, "math": true, "template": true, "noscript": true,
}

var voidTags = map[string]bool{"br": true}

// safeURL permits only http, https, mailto, and relative links.
func safeURL(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	if unsafeURL.MatchString(trimmed) {
		return false
	}
	lowered := strings.ToLower(trimmed)
	if strings.HasPrefix(lowered, "http://") || strings.HasPrefix(lowered, "https://") || strings.HasPrefix(lowered, "mailto:") {
		return true
	}
	// Reject protocol-relative and any other scheme-like prefix.
	if strings.HasPrefix(trimmed, "//") || strings.Contains(trimmed, ":") {
		return false
	}
	return true
}

// skipElement returns the index just past the matching close tag, or the end of
// input when no close tag exists. Nesting of the same tag is not tracked, which
// is acceptable because we discard the content entirely either way.
func skipElement(input string, from int, name string) int {
	lower := strings.ToLower(input)
	open := "<" + name
	closeTag := "</" + name
	search := from
	for search < len(input) {
		nextOpen := strings.Index(lower[search:], open)
		nextClose := strings.Index(lower[search:], closeTag)
		if nextClose < 0 {
			return len(input)
		}
		nextClose += search
		// If another open tag appears first, keep looking for the close.
		if nextOpen >= 0 && nextOpen+search < nextClose {
			search = nextOpen + search + len(open)
			continue
		}
		gt := strings.IndexByte(lower[nextClose:], '>')
		if gt < 0 {
			return len(input)
		}
		return nextClose + gt + 1
	}
	return len(input)
}

func parseTag(tag string) (name string, closing, selfClosing bool, attrs []string) {
	inner := strings.TrimSpace(tag)
	inner = strings.TrimPrefix(inner, "<")
	inner = strings.TrimSuffix(inner, ">")
	inner = strings.TrimSuffix(strings.TrimSpace(inner), "/")
	selfClosing = strings.HasSuffix(strings.TrimSpace(tag), "/>")
	if strings.HasPrefix(inner, "/") {
		closing = true
		inner = strings.TrimPrefix(inner, "/")
	}
	parts := strings.Fields(inner)
	if len(parts) == 0 {
		return "", false, false, nil
	}
	name = strings.ToLower(parts[0])
	if idx := strings.Index(name, "/"); idx > 0 {
		name = name[:idx]
	}
	return name, closing, selfClosing, parts[1:]
}

func splitAttr(attr string) (string, string) {
	idx := strings.Index(attr, "=")
	if idx < 0 {
		return strings.ToLower(attr), ""
	}
	key := strings.ToLower(strings.TrimSpace(attr[:idx]))
	value := strings.Trim(strings.TrimSpace(attr[idx+1:]), `"'`)
	return key, html.UnescapeString(value)
}

type Input struct {
	Title     string     `json:"title"`
	BodyText  string     `json:"body_text"`
	BodyHTML  string     `json:"body_html"`
	Severity  string     `json:"severity"`
	Status    string     `json:"status"`
	Audience  Audience   `json:"audience"`
	PublishAt *time.Time `json:"publish_at"`
}

// Create stores a new announcement and its first version.
func (s *Service) Create(ctx context.Context, actor string, in Input) (Announcement, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.BodyText = strings.TrimSpace(in.BodyText)
	if in.Title == "" || len(in.Title) > 200 {
		return Announcement{}, errors.New("title is required and must be under 200 characters")
	}
	if in.BodyText == "" || len(in.BodyText) > 20000 {
		return Announcement{}, errors.New("body_text is required and must be under 20000 characters")
	}
	if in.Severity == "" {
		in.Severity = "info"
	}
	if in.Severity != "info" && in.Severity != "critical" {
		return Announcement{}, errors.New("severity must be info or critical")
	}
	if in.Status == "" {
		in.Status = "draft"
	}
	switch in.Status {
	case "draft", "scheduled", "published":
	default:
		return Announcement{}, errors.New("status must be draft, scheduled, or published")
	}
	if in.Status == "scheduled" && in.PublishAt == nil {
		return Announcement{}, errors.New("a scheduled announcement requires publish_at")
	}
	if err := in.Audience.Validate(); err != nil {
		return Announcement{}, err
	}
	in.BodyHTML = SanitizeHTML(in.BodyHTML)
	audience, _ := json.Marshal(in.Audience)

	var out Announcement
	err := db.WithTenantUser(ctx, s.admin(), "", actor, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `INSERT INTO announcements (title, body_text, body_html, status, severity, audience, publish_at, created_by_user_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,'')::uuid) RETURNING id::text, created_at`,
			in.Title, in.BodyText, in.BodyHTML, in.Status, in.Severity, string(audience), in.PublishAt, actor).
			Scan(&out.ID, &out.CreatedAt); err != nil {
			return err
		}
		if in.Status == "published" {
			if err := tx.QueryRowContext(ctx, `UPDATE announcements SET published_at=now() WHERE id=$1 RETURNING published_at`, out.ID).Scan(&out.PublishedAt); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO announcement_versions (announcement_id, version, title, body_text, body_html, severity, status, changed_by_user_id)
			VALUES ($1,1,$2,$3,$4,$5,$6,NULLIF($7,'')::uuid)`, out.ID, in.Title, in.BodyText, in.BodyHTML, in.Severity, in.Status, actor); err != nil {
			return err
		}
		out.Title, out.BodyText, out.BodyHTML, out.Status, out.Severity = in.Title, in.BodyText, in.BodyHTML, in.Status, in.Severity
		out.Audience, out.PublishAt, out.CurrentVersion = in.Audience, in.PublishAt, 1
		return audit.Append(ctx, tx, audit.Event{ActorUserID: actor, Action: "announcement.created", ResourceType: "announcement", ResourceID: out.ID, NewState: in})
	})
	if err != nil {
		return Announcement{}, err
	}
	// Publishing resolves the audience into concrete targets so the inbox can be
	// filtered per tenant without re-evaluating segmentation rules at read time.
	if in.Status == "published" {
		if err := s.publish(ctx, actor, out.ID, out.Title, out.Severity, in.Audience); err != nil {
			return Announcement{}, err
		}
		out.Status, out.PublishedAt = "published", ptrTime(time.Now().UTC())
	}
	return out, nil
}

func ptrTime(t time.Time) *time.Time { return &t }

func (s *Service) admin() *sql.DB {
	if s.Admin != nil {
		return s.Admin
	}
	return s.DB
}

// Update creates a new version, preserving history.
func (s *Service) Update(ctx context.Context, actor, id string, in Input) (Announcement, error) {
	if in.Title == "" || in.BodyText == "" {
		return Announcement{}, errors.New("title and body_text are required")
	}
	if err := in.Audience.Validate(); err != nil {
		return Announcement{}, err
	}
	in.BodyHTML = SanitizeHTML(in.BodyHTML)
	if in.Severity == "" {
		in.Severity = "info"
	}
	audience, _ := json.Marshal(in.Audience)
	var out Announcement
	err := db.WithTenantUser(ctx, s.admin(), "", actor, func(tx *sql.Tx) error {
		var currentVersion int
		var oldTitle string
		if err := tx.QueryRowContext(ctx, `SELECT current_version, title FROM announcements WHERE id=$1 FOR UPDATE`, id).
			Scan(&currentVersion, &oldTitle); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		next := currentVersion + 1
		if err := tx.QueryRowContext(ctx, `UPDATE announcements SET title=$2, body_text=$3, body_html=$4, severity=$5, audience=$6,
			publish_at=$7, status=$8, current_version=$9, updated_at=now() WHERE id=$1
			RETURNING id::text, status, publish_at, published_at, current_version, created_at`,
			id, in.Title, in.BodyText, in.BodyHTML, in.Severity, string(audience), in.PublishAt, in.Status, next).
			Scan(&out.ID, &out.Status, &out.PublishAt, &out.PublishedAt, &out.CurrentVersion, &out.CreatedAt); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO announcement_versions (announcement_id, version, title, body_text, body_html, severity, status, changed_by_user_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,'')::uuid)`, id, next, in.Title, in.BodyText, in.BodyHTML, in.Severity, in.Status, actor); err != nil {
			return err
		}
		out.Title, out.BodyText, out.BodyHTML, out.Severity, out.Audience, out.PublishAt = in.Title, in.BodyText, in.BodyHTML, in.Severity, in.Audience, in.PublishAt
		return audit.Append(ctx, tx, audit.Event{ActorUserID: actor, Action: "announcement.updated", ResourceType: "announcement", ResourceID: id,
			OldState: map[string]any{"title": oldTitle, "version": currentVersion}, NewState: map[string]any{"title": in.Title, "version": next}})
	})
	return out, err
}

// Archive removes an announcement from circulation.
func (s *Service) Archive(ctx context.Context, actor, id string) error {
	return db.WithTenantUser(ctx, s.admin(), "", actor, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE announcements SET status='archived', updated_at=now() WHERE id=$1 AND status<>'archived'`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return audit.Append(ctx, tx, audit.Event{ActorUserID: actor, Action: "announcement.archived", ResourceType: "announcement", ResourceID: id})
	})
}

func (s *Service) List(ctx context.Context, status string) ([]Announcement, error) {
	out := []Announcement{}
	query := `SELECT id::text, title, body_text, body_html, status, severity, audience, publish_at, published_at, current_version, created_at
		FROM announcements`
	args := []any{}
	if status != "" {
		query += ` WHERE status=$1`
		args = append(args, status)
	}
	query += ` ORDER BY created_at DESC LIMIT 100`
	rows, err := s.admin().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a Announcement
		var raw []byte
		if err := rows.Scan(&a.ID, &a.Title, &a.BodyText, &a.BodyHTML, &a.Status, &a.Severity, &raw, &a.PublishAt, &a.PublishedAt, &a.CurrentVersion, &a.CreatedAt); err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &a.Audience)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Service) Versions(ctx context.Context, id string) ([]map[string]any, error) {
	rows, err := s.admin().QueryContext(ctx, `SELECT version, title, body_text, severity, status, created_at
		FROM announcement_versions WHERE announcement_id=$1 ORDER BY version DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var version int
		var title, body, severity, status string
		var created time.Time
		if err := rows.Scan(&version, &title, &body, &severity, &status, &created); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"version": version, "title": title, "body_text": body, "severity": severity, "status": status, "created_at": created})
	}
	return out, rows.Err()
}

// publish writes the resolved audience into announcement_targets and marks the
// announcement published. The resolved target list is the tenant-isolation
// boundary for the inbox, so an announcement targeted at some tenants can never
// appear in another tenant's inbox.
func (s *Service) publish(ctx context.Context, actor, id, title, severity string, audience Audience) error {
	ids, err := s.ResolveAudience(ctx, audience)
	if err != nil {
		return err
	}
	return db.WithTenantUser(ctx, s.admin(), "", actor, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE announcements SET status='published', published_at=now() WHERE id=$1 AND status IN ('draft','scheduled')`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM announcement_targets WHERE announcement_id=$1`, id); err != nil {
			return err
		}
		for _, orgID := range ids {
			if _, err := tx.ExecContext(ctx, `INSERT INTO announcement_targets (announcement_id, organization_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, id, orgID); err != nil {
				return err
			}
		}
		return audit.Append(ctx, tx, audit.Event{ActorUserID: actor, Action: "announcement.published", ResourceType: "announcement", ResourceID: id,
			NewState: map[string]any{"organizations": len(ids), "severity": severity, "title": title}})
	})
}

// PublishDue promotes scheduled announcements whose time has arrived.
func (s *Service) PublishDue(ctx context.Context) (int, error) {
	published := 0
	rows, err := s.admin().QueryContext(ctx, `SELECT id::text, title, severity, audience FROM announcements
		WHERE status='scheduled' AND publish_at IS NOT NULL AND publish_at<=now()`)
	if err != nil {
		return 0, err
	}
	type due struct {
		id, title, severity string
		audience            Audience
	}
	pending := []due{}
	for rows.Next() {
		var d due
		var raw []byte
		if err := rows.Scan(&d.id, &d.title, &d.severity, &raw); err != nil {
			rows.Close()
			return 0, err
		}
		_ = json.Unmarshal(raw, &d.audience)
		pending = append(pending, d)
	}
	rows.Close()
	for _, d := range pending {
		if err := s.publish(ctx, "", d.id, d.title, d.severity, d.audience); err != nil {
			return published, err
		}
		published++
	}
	return published, nil
}

// ResolveAudience returns the organization IDs an announcement targets.
func (s *Service) ResolveAudience(ctx context.Context, audience Audience) ([]string, error) {
	query := `SELECT o.id::text FROM organizations o WHERE 1=1`
	args := []any{}
	add := func(clause string, value any) {
		args = append(args, value)
		query += fmt.Sprintf(" AND "+clause, len(args))
	}
	if len(audience.OrganizationIDs) > 0 {
		add("o.id = ANY($%d::uuid[])", pqStringArray(audience.OrganizationIDs))
	}
	if len(audience.PlanCodes) > 0 {
		add("o.plan_code = ANY($%d::text[])", pqStringArray(audience.PlanCodes))
	}
	if len(audience.BusinessCategories) > 0 {
		add("o.business_category = ANY($%d::text[])", pqStringArray(audience.BusinessCategories))
	}
	if audience.BetaAccess != nil {
		add("o.beta_access = $%d", *audience.BetaAccess)
	}
	if audience.ConnectedOnly != nil && *audience.ConnectedOnly {
		query += ` AND EXISTS (SELECT 1 FROM store_connections c WHERE c.organization_id=o.id AND c.status='connected')`
	}
	if audience.ActiveWithinDays != nil {
		add(`o.last_active_at > now() - ($%d * interval '1 day')`, *audience.ActiveWithinDays)
	}
	if audience.MinimumProducts != nil {
		add(`(SELECT COUNT(*) FROM products p WHERE p.organization_id=o.id AND p.status='published') >= $%d`, *audience.MinimumProducts)
	}
	if audience.MinimumTeamMembers != nil {
		add(`(SELECT COUNT(*) FROM memberships m WHERE m.organization_id=o.id AND m.status='active') >= $%d`, *audience.MinimumTeamMembers)
	}
	rows, err := s.admin().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// pqStringArray renders a Go slice as a PostgreSQL array literal. Backslashes
// must be escaped before quotes, otherwise the backslash introduced by quote
// escaping gets doubled.
func pqStringArray(values []string) string {
	if len(values) == 0 {
		return "{}"
	}
	escaped := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.ReplaceAll(v, `\`, `\\`)
		v = strings.ReplaceAll(v, `"`, `\"`)
		escaped = append(escaped, `"`+v+`"`)
	}
	return "{" + strings.Join(escaped, ",") + "}"
}

// Inbox returns announcements targeted at one organization, with read state.
// Audience is matched against the resolved target list captured at publish time,
// so a targeted announcement can never appear in an untargeted tenant's inbox.
func (s *Service) Inbox(ctx context.Context, orgID, userID string) ([]Announcement, error) {
	out := []Announcement{}
	rows, err := s.admin().QueryContext(ctx, `SELECT a.id::text, a.title, a.body_text, a.body_html, a.status, a.severity,
		a.publish_at, a.published_at, a.current_version, a.created_at, r.read_at, r.acknowledged_at
		FROM announcements a
		JOIN announcement_targets t ON t.announcement_id=a.id AND t.organization_id=$1
		LEFT JOIN announcement_reads r ON r.announcement_id=a.id AND r.user_id=$2
		WHERE a.status='published'
		ORDER BY (a.severity='critical') DESC, a.published_at DESC NULLS LAST, a.created_at DESC LIMIT 50`, orgID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a Announcement
		if err := rows.Scan(&a.ID, &a.Title, &a.BodyText, &a.BodyHTML, &a.Status, &a.Severity,
			&a.PublishAt, &a.PublishedAt, &a.CurrentVersion, &a.CreatedAt, &a.ReadAt, &a.AcknowledgedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// MarkRead records read and optional acknowledgement.
func (s *Service) MarkRead(ctx context.Context, userID, announcementID string, acknowledge bool) error {
	if s.DB == nil {
		return errors.New("announcements are not configured")
	}
	return db.WithTenantUser(ctx, s.DB, "", userID, func(tx *sql.Tx) error {
		if acknowledge {
			_, err := tx.ExecContext(ctx, `INSERT INTO announcement_reads (announcement_id, user_id, acknowledged_at)
				VALUES ($1,$2,now()) ON CONFLICT (announcement_id, user_id) DO UPDATE SET
				read_at=COALESCE(announcement_reads.read_at, now()), acknowledged_at=COALESCE(announcement_reads.acknowledged_at, now())`, announcementID, userID)
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO announcement_reads (announcement_id, user_id) VALUES ($1,$2)
			ON CONFLICT (announcement_id, user_id) DO UPDATE SET read_at=COALESCE(announcement_reads.read_at, now())`, announcementID, userID)
		return err
	})
}

// RunWorker publishes scheduled announcements when their time arrives.
func (s *Service) RunWorker(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.PublishDue(ctx); err != nil {
				// Publishing is retried on the next tick; nothing is lost.
				continue
			}
		}
	}
}
