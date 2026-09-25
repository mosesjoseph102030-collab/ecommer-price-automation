// Package feedback implements the private feedback thread: owner submissions,
// support replies, a governed status flow, duplicate merge, and insight tags.
package feedback

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"automation/internal/db"
)

var (
	ErrNotFound = errors.New("feedback ticket not found")
)

// statusFlow is the governed lifecycle. Transitions are explicit so a ticket
// cannot skip stages or move backwards into an invalid state.
//
// The specification lists the flow as Submitted → Triaged → Needs information →
// Planned → In progress → Released → Declined → Closed. Declining is also
// permitted from triaged, planned, and in progress, because a request is
// normally declined before any release work begins; both readings are honoured.
var statusFlow = map[string][]string{
	"submitted":         {"triaged", "closed"},
	"triaged":           {"needs_information", "planned", "in_progress", "declined", "closed"},
	"needs_information": {"submitted", "closed"},
	"planned":           {"in_progress", "declined", "closed"},
	"in_progress":       {"released", "declined", "closed"},
	"released":          {"declined", "closed"},
	"declined":          {"closed"},
	"closed":            {},
}

var validKinds = map[string]bool{"bug": true, "feature_request": true, "improvement": true}
var validPriorities = map[string]bool{"low": true, "normal": true, "high": true, "urgent": true}

// CanTransition reports whether from -> to is a permitted move.
func CanTransition(from, to string) bool {
	allowed, ok := statusFlow[from]
	if !ok {
		return false
	}
	for _, candidate := range allowed {
		if candidate == to {
			return true
		}
	}
	return false
}

// NextStatuses lists the permitted next states for a ticket.
func NextStatuses(status string) []string {
	return append([]string(nil), statusFlow[status]...)
}

type Ticket struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	Subject     string    `json:"subject"`
	Body        string    `json:"body"`
	Status      string    `json:"status"`
	Priority    string    `json:"priority"`
	DuplicateOf *string   `json:"duplicate_of,omitempty"`
	InsightTags []string  `json:"insight_tags"`
	CreatedBy   string    `json:"created_by_user_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Messages    []Message `json:"messages,omitempty"`
	FileIDs     []string  `json:"file_ids,omitempty"`
	NextStates  []string  `json:"next_states,omitempty"`
}

type Message struct {
	ID         string    `json:"id"`
	TicketID   string    `json:"ticket_id"`
	AuthorKind string    `json:"author_kind"`
	AuthorID   string    `json:"author_user_id,omitempty"`
	Body       string    `json:"body"`
	Internal   bool      `json:"internal"`
	CreatedAt  time.Time `json:"created_at"`
}

type Service struct {
	DB    *sql.DB
	Admin *sql.DB
}

type CreateInput struct {
	Kind        string   `json:"kind"`
	Subject     string   `json:"subject"`
	Body        string   `json:"body"`
	FileIDs     []string `json:"file_ids"`
	InsightTags []string `json:"insight_tags"`
}

// Create opens a private ticket for the tenant.
func (s *Service) Create(ctx context.Context, orgID, actor string, in CreateInput) (Ticket, error) {
	in.Kind = strings.ToLower(strings.TrimSpace(in.Kind))
	in.Subject = strings.TrimSpace(in.Subject)
	in.Body = strings.TrimSpace(in.Body)
	if !validKinds[in.Kind] {
		return Ticket{}, errors.New("kind must be bug, feature_request, or improvement")
	}
	if in.Subject == "" || len(in.Subject) > 200 {
		return Ticket{}, errors.New("subject is required and must be under 200 characters")
	}
	if in.Body == "" || len(in.Body) > 10000 {
		return Ticket{}, errors.New("body is required and must be under 10000 characters")
	}
	if len(in.FileIDs) > 10 {
		return Ticket{}, errors.New("at most 10 attachments are allowed")
	}
	var out Ticket
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `INSERT INTO feedback_tickets (organization_id, created_by_user_id, kind, subject, body, insight_tags)
			VALUES ($1,NULLIF($2,'')::uuid,$3,$4,$5,$6) RETURNING id::text, created_at, updated_at`,
			orgID, actor, in.Kind, in.Subject, in.Body, pqTextArray(in.InsightTags)).Scan(&out.ID, &out.CreatedAt, &out.UpdatedAt); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO feedback_events (ticket_id, from_status, to_status, actor_user_id) VALUES ($1,'','submitted',NULLIF($2,'')::uuid)`, out.ID, actor); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO feedback_messages (ticket_id, author_user_id, author_kind, body) VALUES ($1,NULLIF($2,'')::uuid,'owner',$3)`, out.ID, actor, in.Body); err != nil {
			return err
		}
		for _, fileID := range in.FileIDs {
			// Attachments must belong to this tenant and be clean.
			var n int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM files WHERE organization_id=$1 AND id=$2 AND status='clean'`, orgID, fileID).Scan(&n); err != nil {
				return err
			}
			if n == 0 {
				return fmt.Errorf("attachment %s is not available", fileID)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO feedback_files (ticket_id, file_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, out.ID, fileID); err != nil {
				return err
			}
		}
		out.Kind, out.Subject, out.Body, out.Status, out.Priority, out.CreatedBy = in.Kind, in.Subject, in.Body, "submitted", "normal", actor
		out.InsightTags, out.NextStates = in.InsightTags, NextStatuses("submitted")
		return nil
	})
	return out, err
}

// List returns the tenant's tickets. A tenant only ever sees its own tickets.
func (s *Service) List(ctx context.Context, orgID, status string) ([]Ticket, error) {
	query := `SELECT t.id::text, t.kind, t.subject, t.status, t.priority, t.duplicate_of::text, t.insight_tags,
		t.created_at, t.updated_at
		FROM feedback_tickets t WHERE t.organization_id=$1`
	args := []any{orgID}
	if status != "" {
		args = append(args, status)
		query += fmt.Sprintf(" AND t.status=$%d", len(args))
	}
	query += " ORDER BY t.created_at DESC LIMIT 100"
	out := []Ticket{}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var t Ticket
			var duplicate sql.NullString
			if err := rows.Scan(&t.ID, &t.Kind, &t.Subject, &t.Status, &t.Priority, &duplicate, &t.InsightTags, &t.CreatedAt, &t.UpdatedAt); err != nil {
				return err
			}
			if duplicate.Valid {
				t.DuplicateOf = &duplicate.String
			}
			t.NextStates = NextStatuses(t.Status)
			out = append(out, t)
		}
		return rows.Err()
	})
	return out, err
}

// Get returns one ticket with its thread, scoped to the tenant.
func (s *Service) Get(ctx context.Context, orgID, id string) (Ticket, error) {
	var t Ticket
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT id::text, kind, subject, body, status, priority, duplicate_of::text, insight_tags, created_at, updated_at
			FROM feedback_tickets WHERE organization_id=$1 AND id=$2`, orgID, id).
			Scan(&t.ID, &t.Kind, &t.Subject, &t.Body, &t.Status, &t.Priority, &t.DuplicateOf, &t.InsightTags, &t.CreatedAt, &t.UpdatedAt); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT id::text, author_kind, COALESCE(author_user_id::text,''), body, internal, created_at
			FROM feedback_messages WHERE ticket_id=$1 ORDER BY created_at`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var m Message
			if err := rows.Scan(&m.ID, &m.AuthorKind, &m.AuthorID, &m.Body, &m.Internal, &m.CreatedAt); err != nil {
				return err
			}
			// Internal notes are support-only and never shown to the owner.
			if m.Internal {
				continue
			}
			t.Messages = append(t.Messages, m)
		}
		fileRows, err := tx.QueryContext(ctx, `SELECT file_id::text FROM feedback_files WHERE ticket_id=$1`, id)
		if err != nil {
			return err
		}
		defer fileRows.Close()
		for fileRows.Next() {
			var fileID string
			if err := fileRows.Scan(&fileID); err != nil {
				return err
			}
			t.FileIDs = append(t.FileIDs, fileID)
		}
		t.NextStates = NextStatuses(t.Status)
		return fileRows.Err()
	})
	return t, err
}

// Reply adds a message to the thread. Owners cannot create internal notes.
func (s *Service) Reply(ctx context.Context, orgID, actor, ticketID, body string, asSupport, internal bool) (Message, error) {
	body = strings.TrimSpace(body)
	if body == "" || len(body) > 10000 {
		return Message{}, errors.New("body is required and must be under 10000 characters")
	}
	var out Message
	kind := "owner"
	if asSupport {
		kind = "support"
	} else if internal {
		return Message{}, errors.New("only support agents may write internal notes")
	}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM feedback_tickets WHERE organization_id=$1 AND id=$2`, orgID, ticketID).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		if err := tx.QueryRowContext(ctx, `INSERT INTO feedback_messages (ticket_id, author_user_id, author_kind, body, internal)
			VALUES ($1,NULLIF($2,'')::uuid,$3,$4,$5) RETURNING id::text, created_at`, ticketID, actor, kind, body, internal).
			Scan(&out.ID, &out.CreatedAt); err != nil {
			return err
		}
		out.TicketID, out.AuthorKind, out.AuthorID, out.Body, out.Internal = ticketID, kind, actor, body, internal
		return nil
	})
	return out, err
}

// Transition moves a ticket through the governed status flow.
func (s *Service) Transition(ctx context.Context, orgID, actor, ticketID, to, note string) (Ticket, error) {
	to = strings.ToLower(strings.TrimSpace(to))
	if _, ok := statusFlow[to]; !ok {
		return Ticket{}, errors.New("unknown status")
	}
	var out Ticket
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		var from string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM feedback_tickets WHERE organization_id=$1 AND id=$2 FOR UPDATE`, orgID, ticketID).Scan(&from); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if !CanTransition(from, to) {
			return fmt.Errorf("cannot move a ticket from %s to %s", from, to)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE feedback_tickets SET status=$3, updated_at=now() WHERE organization_id=$1 AND id=$2`, orgID, ticketID, to); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO feedback_events (ticket_id, from_status, to_status, actor_user_id) VALUES ($1,$2,$3,NULLIF($4,'')::uuid)`, ticketID, from, to, actor); err != nil {
			return err
		}
		if strings.TrimSpace(note) != "" {
			if _, err := tx.ExecContext(ctx, `INSERT INTO feedback_messages (ticket_id, author_user_id, author_kind, body, internal)
				VALUES ($1,NULLIF($2,'')::uuid,'support',$3,true)`, ticketID, actor, note); err != nil {
				return err
			}
		}
		return tx.QueryRowContext(ctx, `SELECT id::text, kind, subject, status, priority, updated_at FROM feedback_tickets WHERE organization_id=$1 AND id=$2`, orgID, ticketID).
			Scan(&out.ID, &out.Kind, &out.Subject, &out.Status, &out.Priority, &out.UpdatedAt)
	})
	if err != nil {
		return Ticket{}, err
	}
	out.NextStates = NextStatuses(out.Status)
	return out, nil
}

// MergeDuplicate points a ticket at a canonical one.
func (s *Service) MergeDuplicate(ctx context.Context, orgID, actor, ticketID, canonicalID string) error {
	if ticketID == canonicalID {
		return errors.New("a ticket cannot be merged into itself")
	}
	return db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM feedback_tickets WHERE organization_id=$1 AND id=$2 AND id<>$3`, orgID, ticketID, canonicalID).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		if _, err := tx.ExecContext(ctx, `UPDATE feedback_tickets SET duplicate_of=$3, status='closed', updated_at=now() WHERE organization_id=$1 AND id=$2`, orgID, ticketID, canonicalID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO feedback_events (ticket_id, from_status, to_status, actor_user_id) VALUES ($1,'submitted','closed',NULLIF($2,'')::uuid)`, ticketID, actor)
		return err
	})
}

// SetInsightTags records product-insight tags used for feedback analytics.
func (s *Service) SetInsightTags(ctx context.Context, orgID, ticketID string, tags []string) error {
	if len(tags) > 20 {
		return errors.New("at most 20 insight tags are allowed")
	}
	cleaned := []string{}
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag != "" && len(tag) <= 40 {
			cleaned = append(cleaned, tag)
		}
	}
	return db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE feedback_tickets SET insight_tags=$3, updated_at=now() WHERE organization_id=$1 AND id=$2`, orgID, ticketID, pqTextArray(cleaned))
		return err
	})
}

// AdminReply lets a support agent reply on any tenant's ticket. The owning
// organization is read from the ticket itself, then the write is tenant-scoped
// so it cannot touch another tenant's data.
func (s *Service) AdminReply(ctx context.Context, actor, ticketID, body string, internal bool) (Message, error) {
	body = strings.TrimSpace(body)
	if body == "" || len(body) > 10000 {
		return Message{}, errors.New("body is required and must be under 10000 characters")
	}
	var orgID string
	if s.Admin == nil {
		return Message{}, errors.New("support database role is not configured")
	}
	if err := s.Admin.QueryRowContext(ctx, `SELECT organization_id::text FROM feedback_tickets WHERE id=$1`, ticketID).Scan(&orgID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Message{}, ErrNotFound
		}
		return Message{}, err
	}
	// Writes go through the tenant-scoped role with the owning org set.
	var out Message
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM feedback_tickets WHERE organization_id=$1 AND id=$2`, orgID, ticketID).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		if err := tx.QueryRowContext(ctx, `INSERT INTO feedback_messages (ticket_id, author_user_id, author_kind, body, internal)
			VALUES ($1,NULLIF($2,'')::uuid,'support',$3,$4) RETURNING id::text, created_at`, ticketID, actor, body, internal).
			Scan(&out.ID, &out.CreatedAt); err != nil {
			return err
		}
		out.TicketID, out.AuthorKind, out.AuthorID, out.Body, out.Internal = ticketID, "support", actor, body, internal
		return nil
	})
	return out, err
}

func pqTextArray(values []string) string {
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
