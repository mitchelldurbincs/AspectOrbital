package accountability

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

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
	return s.GetByID(ctx, id)
}

func (s *Service) GetByID(ctx context.Context, id int64) (Commitment, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,user_id,task,created_at,deadline,snoozed_until,policy_preset,policy_engine,policy_config,status,proof_metadata,updated_at FROM commitments WHERE id=? LIMIT 1;`, id)
	commitment, err := scanCommitment(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Commitment{}, errors.New("not found")
		}
		return Commitment{}, err
	}
	return commitment, nil
}

func (s *Service) ActiveForUser(ctx context.Context, userID string) (Commitment, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,user_id,task,created_at,deadline,snoozed_until,policy_preset,policy_engine,policy_config,status,proof_metadata,updated_at FROM commitments WHERE user_id=? AND status='pending' LIMIT 1;`, userID)
	commitment, err := scanCommitment(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Commitment{}, errors.New("not found")
		}
		return Commitment{}, err
	}
	return commitment, nil
}

func (s *Service) StatusForUser(ctx context.Context, userID string) (Commitment, error) {
	return s.ActiveForUser(ctx, userID)
}

func (s *Service) Cancel(ctx context.Context, userID string) (Commitment, error) {
	active, err := s.ActiveForUser(ctx, userID)
	if err != nil {
		return Commitment{}, err
	}
	now := s.now()
	_, err = s.db.ExecContext(ctx, `UPDATE commitments SET status='canceled',updated_at=? WHERE id=? AND status='pending';`, ts(now), active.ID)
	if err != nil {
		return Commitment{}, err
	}
	return s.GetByID(ctx, active.ID)
}

func (s *Service) Snooze(ctx context.Context, userID string, duration, maxDuration time.Duration) (Commitment, error) {
	if duration <= 0 {
		return Commitment{}, errors.New("snooze duration must be positive")
	}
	if maxDuration > 0 && duration > maxDuration {
		return Commitment{}, fmt.Errorf("snooze duration cannot exceed %s", maxDuration)
	}
	active, err := s.ActiveForUser(ctx, userID)
	if err != nil {
		return Commitment{}, err
	}
	now := s.now()
	snoozedUntil := now.Add(duration).UTC()
	_, err = s.db.ExecContext(ctx, `UPDATE commitments SET snoozed_until=?,updated_at=? WHERE id=? AND status='pending';`, ts(snoozedUntil), ts(now), active.ID)
	if err != nil {
		return Commitment{}, err
	}
	return s.GetByID(ctx, active.ID)
}

func (s *Service) SubmitProof(ctx context.Context, userID string, submission ProofSubmission) (Commitment, error) {
	active, err := s.ActiveForUser(ctx, userID)
	if err != nil {
		return Commitment{}, err
	}
	now := s.now()

	submission.Text = strings.TrimSpace(submission.Text)
	submission.Verdict = strings.TrimSpace(submission.Verdict)
	meta, _ := json.Marshal(submission)
	status := StatusSuccess
	if !active.Deadline.After(now) {
		status = StatusFailed
	}
	result, err := s.db.ExecContext(ctx, `UPDATE commitments SET status=?,proof_metadata=?,snoozed_until='',updated_at=? WHERE id=? AND status='pending';`, string(status), string(meta), ts(now), active.ID)
	if err != nil {
		return Commitment{}, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return Commitment{}, err
	}
	if rowsAffected == 0 {
		return s.GetByID(ctx, active.ID)
	}
	return s.GetByID(ctx, active.ID)
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
