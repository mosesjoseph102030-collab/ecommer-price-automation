package tenancy

import (
	"context"
	"database/sql"
	"errors"

	automationdb "automation/internal/db"
)

// Resolve checks active membership and returns the member's role IDs.
// Slug -> org resolution happens in the orgs package; this enforces step 3-4
// of the spec flow: confirm membership, load roles. No browser tenant ID trusted.
func Resolve(ctx context.Context, database *sql.DB, userID, orgID string) ([]string, error) {
	roles := []string{}
	err := automationdb.WithTenant(ctx, database, orgID, func(tx *sql.Tx) error {
		var status string
		err := tx.QueryRowContext(ctx,
			`SELECT status FROM memberships WHERE user_id=$1 AND organization_id=$2`,
			userID, orgID).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("TENANT_ACCESS_DENIED")
		}
		if err != nil {
			return err
		}
		if status != "active" {
			return errors.New("TENANT_ACCESS_DENIED: membership not active")
		}
		rows, err := tx.QueryContext(ctx,
			`SELECT role_id FROM member_role_assignments WHERE user_id=$1 AND organization_id=$2`, userID, orgID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var role string
			if err := rows.Scan(&role); err != nil {
				return err
			}
			roles = append(roles, role)
		}
		return rows.Err()
	})
	return roles, err
}
