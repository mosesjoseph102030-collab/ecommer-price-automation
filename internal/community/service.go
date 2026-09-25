package community

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"automation/internal/audit"
	"automation/internal/db"
	"automation/internal/ratelimit"
)

var (
	ErrNotFound     = errors.New("community record not found")
	ErrNotVerified  = errors.New("verified store owner membership is required")
	ErrRateLimited  = errors.New("posting too quickly; try again later")
	ErrBlocked      = errors.New("content was blocked by the community safety policy")
	ErrMuted        = errors.New("you are muted in this room")
	ErrRoomArchived = errors.New("room is archived")
)

type Service struct {
	DB      *sql.DB
	JobDB   *sql.DB
	Admin   *sql.DB
	Limiter *ratelimit.Limiter
	// PostsPerWindow and Window configure anti-spam limits.
	PostsPerWindow int
	Window         time.Duration
}

type Room struct {
	ID                    string `json:"id"`
	Slug                  string `json:"slug"`
	Name                  string `json:"name"`
	Description           string `json:"description"`
	Kind                  string `json:"kind"`
	RequiresVerifiedOwner bool   `json:"requires_verified_owner"`
	Archived              bool   `json:"archived"`
	Muted                 bool   `json:"muted"`
	Member                bool   `json:"member"`
	CanPost               bool   `json:"can_post"`
}

type Profile struct {
	UserID       string `json:"user_id"`
	DisplayName  string `json:"display_name"`
	Headline     string `json:"headline"`
	Bio          string `json:"bio"`
	PrivacyLevel string `json:"privacy_level"`
	MemberSince  int    `json:"rooms"`
}

// Post is the public shape of a community post. It deliberately carries no
// organization, cost, price, or revenue field: community responses are built
// only from user-authored text.
type Post struct {
	ID            string    `json:"id"`
	RoomID        string    `json:"room_id"`
	RoomSlug      string    `json:"room_slug"`
	AuthorUserID  string    `json:"author_user_id"`
	AuthorName    string    `json:"author_name"`
	Body          string    `json:"body"`
	Status        string    `json:"status"`
	ReplyCount    int       `json:"reply_count"`
	ReactionCount int       `json:"reaction_count"`
	Mine          bool      `json:"mine"`
	CreatedAt     time.Time `json:"created_at"`
}

type Reply struct {
	ID            string    `json:"id"`
	PostID        string    `json:"post_id"`
	AuthorUserID  string    `json:"author_user_id"`
	AuthorName    string    `json:"author_name"`
	Body          string    `json:"body"`
	ReactionCount int       `json:"reaction_count"`
	Mine          bool      `json:"mine"`
	CreatedAt     time.Time `json:"created_at"`
}

type ModerationItem struct {
	ReportID     string    `json:"report_id"`
	TargetType   string    `json:"target_type"`
	TargetID     string    `json:"target_id"`
	Excerpt      string    `json:"excerpt"`
	ReporterID   string    `json:"reporter_user_id"`
	Reason       string    `json:"reason"`
	Status       string    `json:"status"`
	AutoCategory string    `json:"auto_category,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

func (s *Service) postLimit() int {
	if s.PostsPerWindow > 0 {
		return s.PostsPerWindow
	}
	return 5
}

func (s *Service) window() time.Duration {
	if s.Window > 0 {
		return s.Window
	}
	return 10 * time.Minute
}

// VerifyOwner confirms the user holds an active store-owner membership in some
// tenant. Verified-owner status is required to enter owner rooms.
func (s *Service) VerifyOwner(ctx context.Context, userID string) (bool, error) {
	database := s.JobDB
	if database == nil {
		database = s.DB
	}
	var n int
	err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM memberships m
		JOIN member_role_assignments r ON r.user_id=m.user_id AND r.organization_id=m.organization_id
		WHERE m.user_id=$1 AND m.status='active' AND r.role_id='store_owner'`, userID).Scan(&n)
	return n > 0, err
}

// ListRooms returns rooms visible to the user, with their posting rights.
func (s *Service) ListRooms(ctx context.Context, userID string) ([]Room, error) {
	verified, err := s.VerifyOwner(ctx, userID)
	if err != nil {
		return nil, err
	}
	database := s.DB
	out := []Room{}
	rows, err := database.QueryContext(ctx, `SELECT id, slug, name, description, kind, requires_verified_owner, archived,
		EXISTS (SELECT 1 FROM community_memberships m WHERE m.room_id=community_rooms.id AND m.user_id=$1)
		FROM community_rooms ORDER BY kind, name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r Room
		if err := rows.Scan(&r.ID, &r.Slug, &r.Name, &r.Description, &r.Kind, &r.RequiresVerifiedOwner, &r.Archived, &r.Member); err != nil {
			return nil, err
		}
		r.CanPost = !r.Archived && r.Kind == "owner" && verified
		out = append(out, r)
	}
	return out, rows.Err()
}

// CreatePost screens content, enforces anti-spam limits, and publishes.
func (s *Service) CreatePost(ctx context.Context, userID, roomID, body string) (Post, error) {
	var out Post
	body = strings.TrimSpace(body)
	if body == "" || len(body) > 5000 {
		return Post{}, errors.New("post must be 1-5000 characters")
	}
	verdict := ScreenContent(body)
	if !verdict.Allowed {
		// Blocked content is not published; it is surfaced to moderators with a
		// redacted excerpt so the decision is reviewable.
		_ = s.recordAutoReport(ctx, userID, "post", "", body, string(verdict.Category))
		return Post{}, fmt.Errorf("%w: %s", ErrBlocked, verdict.Reason)
	}
	if s.Limiter != nil {
		result, err := s.Limiter.Allow(ctx, fmt.Sprintf("community:post:%s", userID), s.postLimit(), s.window())
		if err != nil {
			return Post{}, err
		}
		if !result.Allowed {
			return Post{}, ErrRateLimited
		}
	}
	err := db.WithTenantUser(ctx, s.DB, "", userID, func(tx *sql.Tx) error {
		var kind string
		var requiresOwner, archived bool
		if err := tx.QueryRowContext(ctx, `SELECT kind, requires_verified_owner, archived FROM community_rooms WHERE id=$1`, roomID).
			Scan(&kind, &requiresOwner, &archived); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if archived {
			return ErrRoomArchived
		}
		if kind != "owner" {
			return errors.New("this room is read-only")
		}
		if requiresOwner {
			var muted bool
			_ = tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM community_mutes WHERE user_id=$1 AND room_id=$2 AND (muted_until IS NULL OR muted_until>now()))`, userID, roomID).Scan(&muted)
			if muted {
				return ErrMuted
			}
		}
		if err := tx.QueryRowContext(ctx, `INSERT INTO community_posts (room_id, author_user_id, body) VALUES ($1,$2,$3) RETURNING id, created_at`, roomID, userID, body).
			Scan(&out.ID, &out.CreatedAt); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO community_memberships (room_id, user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, roomID, userID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO community_mentions (target_type, target_id, mentioned_user_id)
			SELECT 'post', $1, p.user_id FROM community_profiles p WHERE p.user_id <> $2 AND p.handle = ANY($3)`, out.ID, userID, extractMentions(body))
		return err
	})
	if err != nil {
		return Post{}, err
	}
	out.RoomID, out.AuthorUserID, out.Body, out.Status, out.Mine = roomID, userID, body, "visible", true
	return out, nil
}

// extractMentions pulls @handle tokens. Email addresses are intentionally NOT
// used for mentions: direct contact details are blocked by the safety screen,
// and handles carry no contact information.
func extractMentions(body string) []string {
	found := []string{}
	for _, field := range strings.Fields(body) {
		field = strings.Trim(field, ".,;:!?()[]\"'")
		if strings.HasPrefix(field, "@") && len(field) > 1 {
			handle := strings.ToLower(strings.TrimPrefix(field, "@"))
			for _, r := range handle {
				if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
					handle = ""
					break
				}
			}
			if len(handle) >= 2 && len(handle) <= 31 {
				found = append(found, handle)
			}
		}
	}
	if len(found) == 0 {
		// Postgres needs a non-empty array for = ANY($n).
		return []string{"__none__"}
	}
	return found
}

func (s *Service) ListPosts(ctx context.Context, userID, roomID string, limit int) ([]Post, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	out := []Post{}
	rows, err := s.DB.QueryContext(ctx, `SELECT p.id, p.room_id, r.slug, p.author_user_id, COALESCE(pr.display_name,''), p.body, p.status,
		(SELECT COUNT(*) FROM community_replies rp WHERE rp.post_id=p.id AND rp.status='visible'),
		(SELECT COUNT(*) FROM community_reactions c WHERE c.target_type='post' AND c.target_id=p.id),
		(p.author_user_id=$1), p.created_at
		FROM community_posts p JOIN community_rooms r ON r.id=p.room_id
		LEFT JOIN community_profiles pr ON pr.user_id=p.author_user_id
		WHERE p.room_id=$2 AND p.status='visible'
		  AND (p.author_user_id=$1
		       OR NOT EXISTS (SELECT 1 FROM community_blocks b WHERE b.blocker_user_id=$1 AND b.blocked_user_id=p.author_user_id))
		  AND NOT EXISTS (SELECT 1 FROM community_blocks b WHERE b.blocker_user_id=p.author_user_id AND b.blocked_user_id IN
		      (SELECT author_user_id FROM community_replies WHERE post_id=p.id))
		ORDER BY p.created_at DESC LIMIT $3`, userID, roomID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p Post
		if err := rows.Scan(&p.ID, &p.RoomID, &p.RoomSlug, &p.AuthorUserID, &p.AuthorName, &p.Body, &p.Status,
			&p.ReplyCount, &p.ReactionCount, &p.Mine, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Service) ListReplies(ctx context.Context, userID, postID string) ([]Reply, error) {
	out := []Reply{}
	rows, err := s.DB.QueryContext(ctx, `SELECT rp.id, rp.post_id, rp.author_user_id, COALESCE(pr.display_name,''), rp.body,
		(SELECT COUNT(*) FROM community_reactions c WHERE c.target_type='reply' AND c.target_id=rp.id),
		(rp.author_user_id=$1), rp.created_at
		FROM community_replies rp LEFT JOIN community_profiles pr ON pr.user_id=rp.author_user_id
		WHERE rp.post_id=$2 AND rp.status='visible'
		  AND NOT EXISTS (SELECT 1 FROM community_blocks b WHERE b.blocker_user_id=$1 AND b.blocked_user_id=rp.author_user_id)
		ORDER BY rp.created_at`, userID, postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r Reply
		if err := rows.Scan(&r.ID, &r.PostID, &r.AuthorUserID, &r.AuthorName, &r.Body, &r.ReactionCount, &r.Mine, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Service) CreateReply(ctx context.Context, userID, postID, body string) (Reply, error) {
	var out Reply
	body = strings.TrimSpace(body)
	if body == "" || len(body) > 3000 {
		return Reply{}, errors.New("reply must be 1-3000 characters")
	}
	verdict := ScreenContent(body)
	if !verdict.Allowed {
		_ = s.recordAutoReport(ctx, userID, "reply", postID, body, string(verdict.Category))
		return Reply{}, fmt.Errorf("%w: %s", ErrBlocked, verdict.Reason)
	}
	if s.Limiter != nil {
		result, err := s.Limiter.Allow(ctx, fmt.Sprintf("community:reply:%s", userID), s.postLimit()*3, s.window())
		if err != nil {
			return Reply{}, err
		}
		if !result.Allowed {
			return Reply{}, ErrRateLimited
		}
	}
	err := db.WithTenantUser(ctx, s.DB, "", userID, func(tx *sql.Tx) error {
		var author string
		if err := tx.QueryRowContext(ctx, `SELECT author_user_id FROM community_posts WHERE id=$1 AND status='visible'`, postID).Scan(&author); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if err := tx.QueryRowContext(ctx, `INSERT INTO community_replies (post_id, author_user_id, body) VALUES ($1,$2,$3) RETURNING id, created_at`, postID, userID, body).
			Scan(&out.ID, &out.CreatedAt); err != nil {
			return err
		}
		_ = author
		_, err := tx.ExecContext(ctx, `INSERT INTO community_mentions (target_type, target_id, mentioned_user_id)
			SELECT 'reply', $1, p.user_id FROM community_profiles p WHERE p.user_id <> $2 AND p.handle = ANY($3)`, out.ID, userID, extractMentions(body))
		return err
	})
	if err != nil {
		return Reply{}, err
	}
	out.PostID, out.AuthorUserID, out.Body, out.Mine = postID, userID, body, true
	return out, nil
}

func (s *Service) React(ctx context.Context, userID, targetType, targetID, kind string) error {
	if targetType != "post" && targetType != "reply" {
		return errors.New("target must be post or reply")
	}
	if kind != "helpful" && kind != "agree" && kind != "insight" {
		return errors.New("invalid reaction kind")
	}
	return db.WithTenantUser(ctx, s.DB, "", userID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO community_reactions (target_type, target_id, user_id, kind) VALUES ($1,$2,$3,$4)
			ON CONFLICT DO NOTHING`, targetType, targetID, userID, kind)
		return err
	})
}

// Report flags content for moderation.
func (s *Service) Report(ctx context.Context, userID, targetType, targetID, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 1000 {
		return errors.New("a report reason is required")
	}
	if targetType != "post" && targetType != "reply" {
		return errors.New("target must be post or reply")
	}
	return db.WithTenantUser(ctx, s.DB, "", userID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO community_reports (target_type, target_id, reporter_user_id, reason)
			VALUES ($1,$2,$3,$4)`, targetType, targetID, userID, reason)
		return err
	})
}

// recordAutoReport stores auto-blocked content for moderator review.
func (s *Service) recordAutoReport(ctx context.Context, userID, targetType, targetID, body, category string) error {
	return db.WithTenantUser(ctx, s.DB, "", userID, func(tx *sql.Tx) error {
		reason := fmt.Sprintf("auto-blocked: %s (%s)", category, Excerpt(body))
		_, err := tx.ExecContext(ctx, `INSERT INTO community_reports (target_type, target_id, reporter_user_id, reason)
			VALUES ($1,$2,$3,$4)`, targetType, targetID, userID, reason)
		return err
	})
}

// ModerationQueue lists open reports for moderators.
func (s *Service) ModerationQueue(ctx context.Context, status string) ([]ModerationItem, error) {
	if status == "" {
		status = "open"
	}
	out := []ModerationItem{}
	rows, err := s.DB.QueryContext(ctx, `SELECT id::text, target_type, target_id::text, reporter_user_id::text, reason, status, created_at
		FROM community_reports WHERE status=$1 ORDER BY created_at LIMIT 100`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var m ModerationItem
		if err := rows.Scan(&m.ReportID, &m.TargetType, &m.TargetID, &m.ReporterID, &m.Reason, &m.Status, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.Excerpt = m.Reason
		m.AutoCategory = categoryFromReason(m.Reason)
		out = append(out, m)
	}
	return out, rows.Err()
}

func categoryFromReason(reason string) string {
	prefix := "auto-blocked: "
	if !strings.HasPrefix(reason, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(reason, prefix)
	if idx := strings.Index(rest, " ("); idx > 0 {
		return rest[:idx]
	}
	return rest
}

// Moderate hides or restores content and closes the report.
func (s *Service) Moderate(ctx context.Context, moderatorID, reportID, action, resolution string) error {
	action = strings.TrimSpace(action)
	// Moderation writes go through the admin role because the shared community
	// tables are read-only for the application role.
	database := s.Admin
	if database == nil {
		return errors.New("moderation database role is not configured")
	}
	resolution = strings.TrimSpace(resolution)
	return db.WithTenantUser(ctx, database, "", moderatorID, func(tx *sql.Tx) error {
		var targetType, targetID string
		if err := tx.QueryRowContext(ctx, `SELECT target_type, target_id::text FROM community_reports WHERE id=$1 FOR UPDATE`, reportID).
			Scan(&targetType, &targetID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		status := "dismissed"
		switch action {
		case "hide":
			status = "actioned"
			table := "community_posts"
			if targetType == "reply" {
				table = "community_replies"
			}
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(`UPDATE %s SET status='hidden' WHERE id=$1`, table), targetID); err != nil {
				return err
			}
			// Hiding the parent post hides the whole thread from readers.
			if targetType == "reply" {
				_, _ = tx.ExecContext(ctx, `UPDATE community_posts SET status='hidden' WHERE id=(SELECT post_id FROM community_replies WHERE id=$1)`, targetID)
			}
		case "restore":
			status = "dismissed"
			table := "community_posts"
			if targetType == "reply" {
				table = "community_replies"
			}
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(`UPDATE %s SET status='visible' WHERE id=$1`, table), targetID); err != nil {
				return err
			}
		case "escalate":
			status = "reviewing"
		default:
			return errors.New("action must be hide, restore, or escalate")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE community_reports SET status=$2, handled_by_user_id=$3, resolution=$4, updated_at=now() WHERE id=$1`,
			reportID, status, moderatorID, resolution); err != nil {
			return err
		}
		return audit.Append(ctx, tx, audit.Event{ActorUserID: moderatorID, Action: "community.moderated",
			ResourceType: "community_report", ResourceID: reportID, NewState: map[string]any{"action": action, "target": targetID}})
	})
}

// Mute silences a user in a room.
func (s *Service) Mute(ctx context.Context, moderatorID, userID, roomID string, until *time.Time, reason string) error {
	if s.Admin == nil {
		return errors.New("moderation database role is not configured")
	}
	return db.WithTenantUser(ctx, s.Admin, "", moderatorID, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO community_mutes (user_id, room_id, muted_until, reason) VALUES ($1,$2,$3,$4)
			ON CONFLICT (user_id, room_id) DO UPDATE SET muted_until=EXCLUDED.muted_until, reason=EXCLUDED.reason`, userID, roomID, until, reason); err != nil {
			return err
		}
		return audit.Append(ctx, tx, audit.Event{ActorUserID: moderatorID, Action: "community.muted", ResourceType: "community_user", ResourceID: userID})
	})
}

// Block hides another user's content from the blocker.
func (s *Service) Block(ctx context.Context, userID, blockedUserID string) error {
	if userID == blockedUserID {
		return errors.New("cannot block yourself")
	}
	return db.WithTenantUser(ctx, s.DB, "", userID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO community_blocks (blocker_user_id, blocked_user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, userID, blockedUserID)
		return err
	})
}

func (s *Service) Unblock(ctx context.Context, userID, blockedUserID string) error {
	return db.WithTenantUser(ctx, s.DB, "", userID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM community_blocks WHERE blocker_user_id=$1 AND blocked_user_id=$2`, userID, blockedUserID)
		return err
	})
}

// UpsertProfile stores the caller's own profile. The profile has no
// organization reference, so it cannot leak tenant data.
func (s *Service) UpsertProfile(ctx context.Context, userID, displayName, headline, bio, privacyLevel string) error {
	privacyLevel = strings.ToLower(strings.TrimSpace(privacyLevel))
	if privacyLevel != "minimal" && privacyLevel != "open" {
		privacyLevel = "minimal"
	}
	if len(displayName) > 80 || len(headline) > 140 || len(bio) > 500 {
		return errors.New("profile field is too long")
	}
	return db.WithTenantUser(ctx, s.DB, "", userID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO community_profiles (user_id, display_name, headline, bio, privacy_level)
			VALUES ($1,$2,$3,$4,$5) ON CONFLICT (user_id) DO UPDATE SET display_name=EXCLUDED.display_name,
			headline=EXCLUDED.headline, bio=EXCLUDED.bio, privacy_level=EXCLUDED.privacy_level, updated_at=now()`,
			userID, strings.TrimSpace(displayName), strings.TrimSpace(headline), strings.TrimSpace(bio), privacyLevel)
		return err
	})
}

// GetProfile returns a caller's own profile view.
func (s *Service) GetProfile(ctx context.Context, userID string) (Profile, error) {
	var p Profile
	err := s.DB.QueryRowContext(ctx, `SELECT user_id::text, display_name, headline, bio, privacy_level,
		(SELECT COUNT(*) FROM community_memberships m WHERE m.user_id=community_profiles.user_id)
		FROM community_profiles WHERE user_id=$1`, userID).
		Scan(&p.UserID, &p.DisplayName, &p.Headline, &p.Bio, &p.PrivacyLevel, &p.MemberSince)
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{UserID: userID, PrivacyLevel: "minimal"}, nil
	}
	return p, err
}
