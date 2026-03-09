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
	GoalSlug      string    `json:"goalSlug"`
	CreatedAt     time.Time `json:"createdAt"`
	Deadline      time.Time `json:"deadline"`
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

type BeeminderWriter interface {
	CreateDatapoint(context.Context, Datapoint) error
}

type Datapoint struct {
	GoalSlug string
	Value    float64
	Comment  string
	Time     time.Time
}

type Service struct {
	dbPath          string
	beeminderClient BeeminderWriter
	now             func() time.Time
	pollInterval    time.Duration
}

func NewService(dbPath string, beeminderClient BeeminderWriter, pollInterval time.Duration) *Service {
	if pollInterval <= 0 {
		pollInterval = 45 * time.Second
	}
	return &Service{dbPath: strings.TrimSpace(dbPath), beeminderClient: beeminderClient, now: func() time.Time { return time.Now().UTC() }, pollInterval: pollInterval}
}

func Bootstrap(_ context.Context, dbPath string) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS commitments (id INTEGER PRIMARY KEY AUTOINCREMENT,user_id TEXT NOT NULL,task TEXT NOT NULL,goal_slug TEXT NOT NULL,created_at TEXT NOT NULL,deadline TEXT NOT NULL,status TEXT NOT NULL,proof_metadata TEXT NOT NULL DEFAULT '',updated_at TEXT NOT NULL);`,
		`CREATE INDEX IF NOT EXISTS idx_commitments_user_status ON commitments(user_id, status);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_commitments_active_one_per_user ON commitments(user_id) WHERE status = 'pending';`,
	}
	for _, stmt := range stmts {
		if _, err := sqliteExec(dbPath, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) Commit(_ context.Context, userID, task, goalSlug string, deadline time.Time) (Commitment, error) {
	now := s.now()
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(task) == "" || strings.TrimSpace(goalSlug) == "" {
		return Commitment{}, errors.New("user, task, and goal slug are required")
	}
	if !deadline.After(now) {
		return Commitment{}, errors.New("deadline must be in the future")
	}
	query := fmt.Sprintf(`INSERT INTO commitments(user_id,task,goal_slug,created_at,deadline,status,updated_at) VALUES(%s,%s,%s,%s,%s,'pending',%s); SELECT last_insert_rowid() AS id;`, q(userID), q(task), q(goalSlug), q(ts(now)), q(ts(deadline.UTC())), q(ts(now)))
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

func (s *Service) SubmitProof(_ context.Context, userID string, attachment AttachmentMetadata) (Commitment, error) {
	active, err := s.ActiveForUser(userID)
	if err != nil {
		return Commitment{}, err
	}
	now := s.now()
	if !now.Before(active.Deadline) {
		_, err := sqliteExec(s.dbPath, fmt.Sprintf(`UPDATE commitments SET status='failed',updated_at=%s WHERE id=%d AND status='pending';`, q(ts(now)), active.ID))
		if err != nil {
			return Commitment{}, err
		}
		_ = s.writeBeeminder(context.Background(), active.GoalSlug, -1, "commitment missed deadline")
		return s.GetByID(active.ID)
	}

	meta, _ := json.Marshal(attachment)
	_, err = sqliteExec(s.dbPath, fmt.Sprintf(`UPDATE commitments SET status='success',proof_metadata=%s,updated_at=%s WHERE id=%d AND status='pending';`, q(string(meta)), q(ts(now)), active.ID))
	if err != nil {
		return Commitment{}, err
	}
	_ = s.writeBeeminder(context.Background(), active.GoalSlug, 1, fmt.Sprintf("proof submitted: %s", attachment.Filename))
	return s.GetByID(active.ID)
}

func (s *Service) ExpireOverdue(_ context.Context) (int64, error) {
	now := s.now()
	rows, err := sqliteQuery(s.dbPath, fmt.Sprintf(`SELECT id,goal_slug FROM commitments WHERE status='pending' AND deadline <= %s;`, q(ts(now))))
	if err != nil {
		return 0, err
	}
	for _, row := range rows {
		id, _ := asInt64(row["id"])
		_, _ = sqliteExec(s.dbPath, fmt.Sprintf(`UPDATE commitments SET status='failed',updated_at=%s WHERE id=%d AND status='pending';`, q(ts(now)), id))
		_ = s.writeBeeminder(context.Background(), asString(row["goal_slug"]), -1, "commitment expired")
	}
	return int64(len(rows)), nil
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

func (s *Service) writeBeeminder(ctx context.Context, goal string, value float64, comment string) error {
	if s.beeminderClient == nil {
		return nil
	}
	return s.beeminderClient.CreateDatapoint(ctx, Datapoint{GoalSlug: goal, Value: value, Comment: comment, Time: s.now()})
}

func sqliteExec(dbPath, sqlText string) ([]map[string]any, error) {
	return sqliteQuery(dbPath, sqlText)
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
	updated, _ := time.Parse(time.RFC3339Nano, asString(row["updated_at"]))
	return Commitment{ID: id, UserID: asString(row["user_id"]), Task: asString(row["task"]), GoalSlug: asString(row["goal_slug"]), CreatedAt: created, Deadline: deadline, Status: Status(asString(row["status"])), ProofMetadata: asString(row["proof_metadata"]), UpdatedAt: updated}, nil
}
