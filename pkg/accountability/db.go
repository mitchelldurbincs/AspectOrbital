package accountability

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

func OpenDB(rawDSN string) (*sql.DB, error) {
	dsn := normalizeSQLiteDSN(rawDSN)
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("db path is required")
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

func Bootstrap(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("db is required")
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS commitments (id INTEGER PRIMARY KEY AUTOINCREMENT,user_id TEXT NOT NULL,task TEXT NOT NULL,goal_slug TEXT NOT NULL,created_at TEXT NOT NULL,deadline TEXT NOT NULL,snoozed_until TEXT NOT NULL DEFAULT '',last_reminder_at TEXT NOT NULL DEFAULT '',last_checkin_at TEXT NOT NULL DEFAULT '',last_checkin_text TEXT NOT NULL DEFAULT '',checkin_quiet_until TEXT NOT NULL DEFAULT '',reminder_count INTEGER NOT NULL DEFAULT 0,policy_preset TEXT NOT NULL DEFAULT '',policy_engine TEXT NOT NULL DEFAULT '',policy_config TEXT NOT NULL DEFAULT '{}',status TEXT NOT NULL,proof_metadata TEXT NOT NULL DEFAULT '',updated_at TEXT NOT NULL);`,
		`CREATE INDEX IF NOT EXISTS idx_commitments_user_status ON commitments(user_id, status);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_commitments_active_one_per_user ON commitments(user_id) WHERE status = 'pending';`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if err := ensureCommitmentColumn(ctx, db, "snoozed_until", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureCommitmentColumn(ctx, db, "last_reminder_at", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureCommitmentColumn(ctx, db, "policy_preset", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureCommitmentColumn(ctx, db, "policy_engine", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureCommitmentColumn(ctx, db, "policy_config", "TEXT NOT NULL DEFAULT '{}'"); err != nil {
		return err
	}
	if err := ensureCommitmentColumn(ctx, db, "last_checkin_at", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureCommitmentColumn(ctx, db, "last_checkin_text", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureCommitmentColumn(ctx, db, "checkin_quiet_until", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureCommitmentColumn(ctx, db, "reminder_count", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	return nil
}

func ensureCommitmentColumn(ctx context.Context, db *sql.DB, columnName, columnDef string) error {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(commitments);`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, colType string
		var notNull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			return err
		}
		if strings.EqualFold(name, columnName) {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	_, err = db.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE commitments ADD COLUMN %s %s;`, columnName, columnDef))
	return err
}

func isUniqueConstraintError(err error) bool {
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "unique") || strings.Contains(lower, "constraint failed")
}

func normalizeSQLiteDSN(raw string) string {
	return strings.TrimSpace(raw)
}
