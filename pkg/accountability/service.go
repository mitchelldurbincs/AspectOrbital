package accountability

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Status string

const (
	StatusPending  Status = "pending"
	StatusSuccess  Status = "success"
	StatusFailed   Status = "failed"
	StatusCanceled Status = "canceled"
)

type Commitment struct {
	ID            int64     `json:"id"`
	UserID        string    `json:"userId"`
	Task          string    `json:"task"`
	CreatedAt     time.Time `json:"createdAt"`
	Deadline      time.Time `json:"deadline"`
	SnoozedUntil  time.Time `json:"snoozedUntil,omitempty"`
	PolicyPreset  string    `json:"policyPreset,omitempty"`
	PolicyEngine  string    `json:"policyEngine,omitempty"`
	PolicyConfig  string    `json:"policyConfig,omitempty"`
	Status        Status    `json:"status"`
	ProofMetadata string    `json:"proofMetadata,omitempty"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type AttachmentMetadata struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	URL         string `json:"url"`
	ContentType string `json:"contentType"`
	Size        int    `json:"size"`
}

type ProofSubmission struct {
	Attachment AttachmentMetadata `json:"attachment,omitempty"`
	Text       string             `json:"text,omitempty"`
	Verdict    string             `json:"verdict,omitempty"`
}

type Service struct {
	db           *sql.DB
	now          func() time.Time
	pollInterval time.Duration
	expiryGrace  time.Duration
}

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

func NewService(db *sql.DB, pollInterval, expiryGrace time.Duration) (*Service, error) {
	if db == nil {
		return nil, errors.New("db is required")
	}
	if pollInterval <= 0 {
		pollInterval = 45 * time.Second
	}
	if expiryGrace < 0 {
		expiryGrace = 0
	}
	return &Service{db: db, now: func() time.Time { return time.Now().UTC() }, pollInterval: pollInterval, expiryGrace: expiryGrace}, nil
}

func Bootstrap(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("db is required")
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS commitments (id INTEGER PRIMARY KEY AUTOINCREMENT,user_id TEXT NOT NULL,task TEXT NOT NULL,goal_slug TEXT NOT NULL,created_at TEXT NOT NULL,deadline TEXT NOT NULL,snoozed_until TEXT NOT NULL DEFAULT '',last_reminder_at TEXT NOT NULL DEFAULT '',policy_preset TEXT NOT NULL DEFAULT '',policy_engine TEXT NOT NULL DEFAULT '',policy_config TEXT NOT NULL DEFAULT '{}',status TEXT NOT NULL,proof_metadata TEXT NOT NULL DEFAULT '',updated_at TEXT NOT NULL);`,
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
	return nil
}

func (s *Service) Commit(ctx context.Context, userID, task string, deadline time.Time) (Commitment, error) {
	return s.CommitWithPolicy(ctx, userID, task, deadline, "", "", "")
}

func (s *Service) CommitWithPolicy(ctx context.Context, userID, task string, deadline time.Time, presetID, engineID, policyConfig string) (Commitment, error) {
	now := s.now()
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(task) == "" {
		return Commitment{}, errors.New("user and task are required")
	}
	if !deadline.After(now) {
		return Commitment{}, errors.New("deadline must be in the future")
	}
	presetID = strings.TrimSpace(presetID)
	engineID = strings.TrimSpace(engineID)
	policyConfig = strings.TrimSpace(policyConfig)
	if policyConfig == "" {
		policyConfig = "{}"
	}

	result, err := s.db.ExecContext(
		ctx,
		`INSERT INTO commitments(user_id,task,goal_slug,created_at,deadline,snoozed_until,last_reminder_at,policy_preset,policy_engine,policy_config,status,updated_at)
		 VALUES(?, ?, '', ?, ?, '', '', ?, ?, ?, 'pending', ?);`,
		userID,
		task,
		ts(now),
		ts(deadline.UTC()),
		presetID,
		engineID,
		policyConfig,
		ts(now),
	)
	if err != nil {
		if isUniqueConstraintError(err) {
			return Commitment{}, errors.New("you already have an active commitment")
		}
		return Commitment{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Commitment{}, err
	}
	return s.GetByID(id)
}

func (s *Service) GetByID(id int64) (Commitment, error) {
	row := s.db.QueryRowContext(context.Background(), `SELECT id,user_id,task,created_at,deadline,snoozed_until,policy_preset,policy_engine,policy_config,status,proof_metadata,updated_at FROM commitments WHERE id=? LIMIT 1;`, id)
	commitment, err := scanCommitment(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Commitment{}, errors.New("not found")
		}
		return Commitment{}, err
	}
	return commitment, nil
}

func (s *Service) ActiveForUser(userID string) (Commitment, error) {
	row := s.db.QueryRowContext(context.Background(), `SELECT id,user_id,task,created_at,deadline,snoozed_until,policy_preset,policy_engine,policy_config,status,proof_metadata,updated_at FROM commitments WHERE user_id=? AND status='pending' LIMIT 1;`, userID)
	commitment, err := scanCommitment(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Commitment{}, errors.New("not found")
		}
		return Commitment{}, err
	}
	return commitment, nil
}

func (s *Service) StatusForUser(_ context.Context, userID string) (Commitment, error) {
	return s.ActiveForUser(userID)
}

func (s *Service) Cancel(_ context.Context, userID string) (Commitment, error) {
	active, err := s.ActiveForUser(userID)
	if err != nil {
		return Commitment{}, err
	}
	now := s.now()
	_, err = s.db.ExecContext(context.Background(), `UPDATE commitments SET status='canceled',updated_at=? WHERE id=? AND status='pending';`, ts(now), active.ID)
	if err != nil {
		return Commitment{}, err
	}
	return s.GetByID(active.ID)
}

func (s *Service) Snooze(_ context.Context, userID string, duration, maxDuration time.Duration) (Commitment, error) {
	if duration <= 0 {
		return Commitment{}, errors.New("snooze duration must be positive")
	}
	if maxDuration > 0 && duration > maxDuration {
		return Commitment{}, fmt.Errorf("snooze duration cannot exceed %s", maxDuration)
	}
	active, err := s.ActiveForUser(userID)
	if err != nil {
		return Commitment{}, err
	}
	now := s.now()
	snoozedUntil := now.Add(duration).UTC()
	_, err = s.db.ExecContext(context.Background(), `UPDATE commitments SET snoozed_until=?,updated_at=? WHERE id=? AND status='pending';`, ts(snoozedUntil), ts(now), active.ID)
	if err != nil {
		return Commitment{}, err
	}
	return s.GetByID(active.ID)
}

func (s *Service) SubmitProof(_ context.Context, userID string, submission ProofSubmission) (Commitment, error) {
	active, err := s.ActiveForUser(userID)
	if err != nil {
		return Commitment{}, err
	}
	now := s.now()

	submission.Text = strings.TrimSpace(submission.Text)
	submission.Verdict = strings.TrimSpace(submission.Verdict)
	meta, _ := json.Marshal(submission)
	_, err = s.db.ExecContext(context.Background(), `UPDATE commitments SET status='success',proof_metadata=?,snoozed_until='',updated_at=? WHERE id=? AND status='pending';`, string(meta), ts(now), active.ID)
	if err != nil {
		return Commitment{}, err
	}
	return s.GetByID(active.ID)
}

func (s *Service) ExpireOverdue(_ context.Context) (int64, error) {
	now := s.now()
	cutoff := now.Add(-s.expiryGrace)
	rows, err := s.db.QueryContext(context.Background(), `SELECT id FROM commitments WHERE status='pending' AND deadline <= ?;`, ts(cutoff))
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var expired int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		if _, err := s.db.ExecContext(context.Background(), `UPDATE commitments SET status='failed',updated_at=? WHERE id=? AND status='pending';`, ts(now), id); err != nil {
			return 0, err
		}
		expired++
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return expired, nil
}

func (s *Service) OverdueNeedingReminder(_ context.Context, reminderInterval time.Duration) ([]Commitment, error) {
	if reminderInterval <= 0 {
		return nil, errors.New("reminder interval must be positive")
	}
	now := s.now()
	earliestReminder := now.Add(-reminderInterval)

	var rows *sql.Rows
	var err error
	if s.expiryGrace > 0 {
		rows, err = s.db.QueryContext(
			context.Background(),
			`SELECT id,user_id,task,created_at,deadline,snoozed_until,policy_preset,policy_engine,policy_config,status,proof_metadata,updated_at
			 FROM commitments
			 WHERE status='pending'
			 AND deadline <= ?
			 AND deadline > ?
			 AND (snoozed_until='' OR snoozed_until <= ?)
			 AND (last_reminder_at='' OR last_reminder_at <= ?);`,
			ts(now),
			ts(now.Add(-s.expiryGrace)),
			ts(now),
			ts(earliestReminder),
		)
	} else {
		rows, err = s.db.QueryContext(
			context.Background(),
			`SELECT id,user_id,task,created_at,deadline,snoozed_until,policy_preset,policy_engine,policy_config,status,proof_metadata,updated_at
			 FROM commitments
			 WHERE status='pending'
			 AND deadline <= ?
			 AND (snoozed_until='' OR snoozed_until <= ?)
			 AND (last_reminder_at='' OR last_reminder_at <= ?);`,
			ts(now),
			ts(now),
			ts(earliestReminder),
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Commitment, 0)
	for rows.Next() {
		commitment, scanErr := scanCommitment(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, commitment)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) MarkReminderSent(_ context.Context, commitmentID int64) error {
	if commitmentID <= 0 {
		return errors.New("commitment id must be positive")
	}
	_, err := s.db.ExecContext(context.Background(), `UPDATE commitments SET last_reminder_at=? WHERE id=? AND status='pending';`, ts(s.now()), commitmentID)
	return err
}

func (s *Service) StartExpiryLoop(ctx context.Context) {
	t := time.NewTicker(s.pollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_, _ = s.ExpireOverdue(ctx)
		}
	}
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

type commitmentScanner interface {
	Scan(dest ...any) error
}

func scanCommitment(scanner commitmentScanner) (Commitment, error) {
	var commitment Commitment
	var createdAtRaw string
	var deadlineRaw string
	var snoozedUntilRaw string
	var policyConfigRaw string
	var statusRaw string
	var updatedAtRaw string

	if err := scanner.Scan(
		&commitment.ID,
		&commitment.UserID,
		&commitment.Task,
		&createdAtRaw,
		&deadlineRaw,
		&snoozedUntilRaw,
		&commitment.PolicyPreset,
		&commitment.PolicyEngine,
		&policyConfigRaw,
		&statusRaw,
		&commitment.ProofMetadata,
		&updatedAtRaw,
	); err != nil {
		return Commitment{}, err
	}

	if policyConfigRaw == "" {
		policyConfigRaw = "{}"
	}
	commitment.PolicyConfig = policyConfigRaw
	commitment.Status = Status(strings.TrimSpace(statusRaw))
	commitment.CreatedAt = parseTimestamp(createdAtRaw)
	commitment.Deadline = parseTimestamp(deadlineRaw)
	commitment.SnoozedUntil = parseTimestamp(snoozedUntilRaw)
	commitment.UpdatedAt = parseTimestamp(updatedAtRaw)

	return commitment, nil
}

func parseTimestamp(raw string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw))
	return parsed
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func isUniqueConstraintError(err error) bool {
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "unique") || strings.Contains(lower, "constraint failed")
}

func normalizeSQLiteDSN(raw string) string {
	return strings.TrimSpace(raw)
}
