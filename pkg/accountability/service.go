package accountability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
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
	dbPath       string
	now          func() time.Time
	pollInterval time.Duration
	expiryGrace  time.Duration
}

func NewService(dbPath string, pollInterval, expiryGrace time.Duration) *Service {
	if pollInterval <= 0 {
		pollInterval = 45 * time.Second
	}
	if expiryGrace < 0 {
		expiryGrace = 0
	}
	return &Service{dbPath: strings.TrimSpace(dbPath), now: func() time.Time { return time.Now().UTC() }, pollInterval: pollInterval, expiryGrace: expiryGrace}
}

func Bootstrap(_ context.Context, dbPath string) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS commitments (id INTEGER PRIMARY KEY AUTOINCREMENT,user_id TEXT NOT NULL,task TEXT NOT NULL,goal_slug TEXT NOT NULL,created_at TEXT NOT NULL,deadline TEXT NOT NULL,snoozed_until TEXT NOT NULL DEFAULT '',last_reminder_at TEXT NOT NULL DEFAULT '',policy_preset TEXT NOT NULL DEFAULT '',policy_engine TEXT NOT NULL DEFAULT '',policy_config TEXT NOT NULL DEFAULT '{}',status TEXT NOT NULL,proof_metadata TEXT NOT NULL DEFAULT '',updated_at TEXT NOT NULL);`,
		`CREATE INDEX IF NOT EXISTS idx_commitments_user_status ON commitments(user_id, status);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_commitments_active_one_per_user ON commitments(user_id) WHERE status = 'pending';`,
	}
	for _, stmt := range stmts {
		if _, err := sqliteExec(dbPath, stmt); err != nil {
			return err
		}
	}
	if err := ensureCommitmentColumn(dbPath, "snoozed_until", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureCommitmentColumn(dbPath, "last_reminder_at", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureCommitmentColumn(dbPath, "policy_preset", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureCommitmentColumn(dbPath, "policy_engine", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureCommitmentColumn(dbPath, "policy_config", "TEXT NOT NULL DEFAULT '{}'"); err != nil {
		return err
	}
	return nil
}

func (s *Service) Commit(_ context.Context, userID, task string, deadline time.Time) (Commitment, error) {
	return s.CommitWithPolicy(context.Background(), userID, task, deadline, "", "", "")
}

func (s *Service) CommitWithPolicy(_ context.Context, userID, task string, deadline time.Time, presetID, engineID, policyConfig string) (Commitment, error) {
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
	query := fmt.Sprintf(`INSERT INTO commitments(user_id,task,goal_slug,created_at,deadline,snoozed_until,last_reminder_at,policy_preset,policy_engine,policy_config,status,updated_at) VALUES(%s,%s,'',%s,%s,'','',%s,%s,%s,'pending',%s); SELECT last_insert_rowid() AS id;`, q(userID), q(task), q(ts(now)), q(ts(deadline.UTC())), q(presetID), q(engineID), q(policyConfig), q(ts(now)))
	rows, err := sqliteQuery(s.dbPath, query)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return Commitment{}, errors.New("you already have an active commitment")
		}
		return Commitment{}, err
	}
	id, _ := asInt64(rows[0]["id"])
	return s.GetByID(id)
}

func (s *Service) GetByID(id int64) (Commitment, error) {
	rows, err := sqliteQuery(s.dbPath, fmt.Sprintf(`SELECT * FROM commitments WHERE id=%d LIMIT 1;`, id))
	if err != nil {
		return Commitment{}, err
	}
	if len(rows) == 0 {
		return Commitment{}, errors.New("not found")
	}
	return mapCommitment(rows[0])
}

func (s *Service) ActiveForUser(userID string) (Commitment, error) {
	rows, err := sqliteQuery(s.dbPath, fmt.Sprintf(`SELECT * FROM commitments WHERE user_id=%s AND status='pending' LIMIT 1;`, q(userID)))
	if err != nil {
		return Commitment{}, err
	}
	if len(rows) == 0 {
		return Commitment{}, errors.New("not found")
	}
	return mapCommitment(rows[0])
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
	_, err = sqliteExec(s.dbPath, fmt.Sprintf(`UPDATE commitments SET status='canceled',updated_at=%s WHERE id=%d AND status='pending';`, q(ts(now)), active.ID))
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
	_, err = sqliteExec(s.dbPath, fmt.Sprintf(`UPDATE commitments SET snoozed_until=%s,updated_at=%s WHERE id=%d AND status='pending';`, q(ts(snoozedUntil)), q(ts(now)), active.ID))
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
	_, err = sqliteExec(s.dbPath, fmt.Sprintf(`UPDATE commitments SET status='success',proof_metadata=%s,snoozed_until='',updated_at=%s WHERE id=%d AND status='pending';`, q(string(meta)), q(ts(now)), active.ID))
	if err != nil {
		return Commitment{}, err
	}
	return s.GetByID(active.ID)
}

func (s *Service) ExpireOverdue(_ context.Context) (int64, error) {
	now := s.now()
	cutoff := now.Add(-s.expiryGrace)
	rows, err := sqliteQuery(s.dbPath, fmt.Sprintf(`SELECT id FROM commitments WHERE status='pending' AND deadline <= %s;`, q(ts(cutoff))))
	if err != nil {
		return 0, err
	}
	for _, row := range rows {
		id, _ := asInt64(row["id"])
		_, _ = sqliteExec(s.dbPath, fmt.Sprintf(`UPDATE commitments SET status='failed',updated_at=%s WHERE id=%d AND status='pending';`, q(ts(now)), id))
	}
	return int64(len(rows)), nil
}

func (s *Service) OverdueNeedingReminder(_ context.Context, reminderInterval time.Duration) ([]Commitment, error) {
	if reminderInterval <= 0 {
		return nil, errors.New("reminder interval must be positive")
	}
	now := s.now()
	earliestReminder := now.Add(-reminderInterval)
	query := fmt.Sprintf(`SELECT * FROM commitments WHERE status='pending' AND deadline <= %s AND (snoozed_until='' OR snoozed_until <= %s) AND (last_reminder_at='' OR last_reminder_at <= %s);`, q(ts(now)), q(ts(now)), q(ts(earliestReminder)))
	if s.expiryGrace > 0 {
		query = fmt.Sprintf(`SELECT * FROM commitments WHERE status='pending' AND deadline <= %s AND deadline > %s AND (snoozed_until='' OR snoozed_until <= %s) AND (last_reminder_at='' OR last_reminder_at <= %s);`, q(ts(now)), q(ts(now.Add(-s.expiryGrace))), q(ts(now)), q(ts(earliestReminder)))
	}
	rows, err := sqliteQuery(s.dbPath, query)
	if err != nil {
		return nil, err
	}
	out := make([]Commitment, 0, len(rows))
	for _, row := range rows {
		commitment, mapErr := mapCommitment(row)
		if mapErr != nil {
			return nil, mapErr
		}
		out = append(out, commitment)
	}
	return out, nil
}

func (s *Service) MarkReminderSent(_ context.Context, commitmentID int64) error {
	if commitmentID <= 0 {
		return errors.New("commitment id must be positive")
	}
	_, err := sqliteExec(s.dbPath, fmt.Sprintf(`UPDATE commitments SET last_reminder_at=%s WHERE id=%d AND status='pending';`, q(ts(s.now())), commitmentID))
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

func sqliteExec(dbPath, sqlText string) ([]map[string]any, error) {
	return sqliteQuery(dbPath, sqlText)
}

func ensureCommitmentColumn(dbPath, columnName, columnDef string) error {
	rows, err := sqliteQuery(dbPath, `PRAGMA table_info(commitments);`)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if strings.EqualFold(asString(row["name"]), columnName) {
			return nil
		}
	}
	_, err = sqliteExec(dbPath, fmt.Sprintf(`ALTER TABLE commitments ADD COLUMN %s %s;`, columnName, columnDef))
	return err
}

func sqliteQuery(dbPath, sqlText string) ([]map[string]any, error) {
	cmd := exec.Command("sqlite3", "-json", dbPath, sqlText)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("sqlite error: %v: %s", err, strings.TrimSpace(string(out)))
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil, nil
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(trimmed), &rows); err != nil {
		return nil, err
	}
	return rows, nil
}
func q(v string) string     { return "'" + strings.ReplaceAll(v, "'", "''") + "'" }
func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
func asString(v any) string { return strings.TrimSpace(fmt.Sprint(v)) }
func asInt64(v any) (int64, error) {
	s := asString(v)
	return strconv.ParseInt(s, 10, 64)
}
func mapCommitment(row map[string]any) (Commitment, error) {
	id, err := asInt64(row["id"])
	if err != nil {
		return Commitment{}, err
	}
	created, _ := time.Parse(time.RFC3339Nano, asString(row["created_at"]))
	deadline, _ := time.Parse(time.RFC3339Nano, asString(row["deadline"]))
	snoozedUntil, _ := time.Parse(time.RFC3339Nano, asString(row["snoozed_until"]))
	updated, _ := time.Parse(time.RFC3339Nano, asString(row["updated_at"]))
	policyConfig := asString(row["policy_config"])
	if policyConfig == "" {
		policyConfig = "{}"
	}
	return Commitment{ID: id, UserID: asString(row["user_id"]), Task: asString(row["task"]), CreatedAt: created, Deadline: deadline, SnoozedUntil: snoozedUntil, PolicyPreset: asString(row["policy_preset"]), PolicyEngine: asString(row["policy_engine"]), PolicyConfig: policyConfig, Status: Status(asString(row["status"])), ProofMetadata: asString(row["proof_metadata"]), UpdatedAt: updated}, nil
}
