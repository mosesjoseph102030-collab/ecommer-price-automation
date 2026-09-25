package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	_ "github.com/lib/pq"
)

// Open connects to Postgres. Caller must set DATABASE_URL.
func Open(url string) (*sql.DB, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return db, nil
}

// RunMigrations applies each *.sql file once in lexical order and records it in
// schema_migrations. Each migration runs atomically.
func RunMigrations(db *sql.DB, dir string) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, f := range files {
		name := filepath.Base(f)
		var exists bool
		if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name=$1)`, name).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("read %s: %w", f, err)
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err = tx.Exec(string(b)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err = tx.Exec(`INSERT INTO schema_migrations(name) VALUES($1)`, name); err != nil {
			tx.Rollback()
			return err
		}
		if err = tx.Commit(); err != nil {
			return fmt.Errorf("commit %s: %w", name, err)
		}
	}
	return nil
}

// SetTenantContext sets the RLS request-scoped tenant for this transaction.
// It is transaction-local and automatically cleared on commit/rollback.
func SetTenantContext(tx *sql.Tx, orgID string) error {
	_, err := tx.Exec(`SELECT set_config('app.current_organization_id', $1, true)`, orgID)
	return err
}

// SetUserContext sets the RLS request-scoped acting user for this transaction.
// It is required by platform-shared tables (announcements, community) that have
// no organization_id and must still be isolated per user.
func SetUserContext(tx *sql.Tx, userID string) error {
	if userID == "" {
		return nil
	}
	_, err := tx.Exec(`SELECT set_config('app.current_user_id', $1, true)`, userID)
	return err
}

// WithTenant runs fn inside a transaction with the RLS tenant context set.
func WithTenant(ctx context.Context, database *sql.DB, orgID string, fn func(*sql.Tx) error) error {
	return WithTenantUser(ctx, database, orgID, "", fn)
}

// WithTenantUser runs fn with both the tenant and acting-user RLS context set.
func WithTenantUser(ctx context.Context, database *sql.DB, orgID, userID string, fn func(*sql.Tx) error) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := SetTenantContext(tx, orgID); err != nil {
		return err
	}
	if err := SetUserContext(tx, userID); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
