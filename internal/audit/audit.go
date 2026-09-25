package audit

import (
	"context"
	"database/sql"
	"encoding/json"
)

// Event is an append-only audit record. Old/New state recorded where appropriate.
type Event struct {
	OrgID        string
	ActorUserID  string
	Action       string
	ResourceType string
	ResourceID   string
	RequestID    string
	OldState     any
	NewState     any
}

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// Append writes one audit row through the supplied DB or transaction.
func Append(ctx context.Context, database execer, e Event) error {
	var oldB, newB []byte
	var err error
	if e.OldState != nil {
		if oldB, err = json.Marshal(e.OldState); err != nil {
			return err
		}
	}
	if e.NewState != nil {
		if newB, err = json.Marshal(e.NewState); err != nil {
			return err
		}
	}
	_, err = database.ExecContext(ctx, `
		INSERT INTO audit_events (organization_id, actor_user_id, action, resource_type, resource_id, request_id, old_state, new_state)
		VALUES (NULLIF($1,'')::uuid, NULLIF($2,'')::uuid, $3, $4, $5, $6, $7::jsonb, $8::jsonb)`,
		nullStr(e.OrgID), nullStr(e.ActorUserID), e.Action, e.ResourceType, e.ResourceID, e.RequestID,
		nullableJSON(oldB), nullableJSON(newB),
	)
	return err
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableJSON(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}
