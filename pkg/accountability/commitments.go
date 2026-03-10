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
	ctx = contextOrBackground(ctx)
	now := s.now()
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(task) == "" {
		return Commitment{}, invalidError("user and task are required")
	}
	if !deadline.After(now) {
		return Commitment{}, invalidError("deadline must be in the future")
	}
	presetID = strings.TrimSpace(presetID)
	engineID = strings.TrimSpace(engineID)
	policyConfig = strings.TrimSpace(policyConfig)
	if policyConfig == "" {
		policyConfig = "{}"
	}

	result, err := s.db.ExecContext(
		ctx,
		insertCommitmentSQL,
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
			return Commitment{}, conflictError("you already have an active commitment")
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
	row := s.db.QueryRowContext(contextOrBackground(ctx), commitmentSelect("WHERE id=? LIMIT 1;"), id)
	commitment, err := scanCommitment(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Commitment{}, notFoundError("commitment not found")
		}
		return Commitment{}, err
	}
	return commitment, nil
}

func (s *Service) ActiveForUser(ctx context.Context, userID string) (Commitment, error) {
	return activeCommitmentForUser(contextOrBackground(ctx), s.db, userID)
}

func activeCommitmentForUser(ctx context.Context, querier commitmentQuerier, userID string) (Commitment, error) {
	row := querier.QueryRowContext(contextOrBackground(ctx), commitmentSelect("WHERE user_id=? AND status='pending' LIMIT 1;"), userID)
	commitment, err := scanCommitment(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Commitment{}, notFoundError("no active commitment")
		}
		return Commitment{}, err
	}
	return commitment, nil
}

func (s *Service) StatusForUser(ctx context.Context, userID string) (Commitment, error) {
	return s.ActiveForUser(ctx, userID)
}

func (s *Service) Cancel(ctx context.Context, userID string) (Commitment, error) {
	ctx = contextOrBackground(ctx)
	return s.mutatePendingCommitment(ctx, userID, func(tx *sql.Tx, active Commitment, now time.Time) error {
		return ensureMutationApplied(tx.ExecContext(ctx, updateCommitmentCanceledSQL, ts(now), active.ID))
	})
}

func (s *Service) Snooze(ctx context.Context, userID string, duration, maxDuration time.Duration) (Commitment, error) {
	ctx = contextOrBackground(ctx)
	if duration <= 0 {
		return Commitment{}, invalidError("snooze duration must be positive")
	}
	if maxDuration > 0 && duration > maxDuration {
		return Commitment{}, invalidError(fmt.Sprintf("snooze duration cannot exceed %s", maxDuration))
	}
	return s.mutatePendingCommitment(ctx, userID, func(tx *sql.Tx, active Commitment, now time.Time) error {
		snoozedUntil := now.Add(duration).UTC()
		return ensureMutationApplied(tx.ExecContext(ctx, updateCommitmentSnoozedSQL, ts(snoozedUntil), ts(now), active.ID))
	})
}

func (s *Service) CheckIn(ctx context.Context, userID, text string, quietPeriod time.Duration) (Commitment, error) {
	ctx = contextOrBackground(ctx)
	text = strings.TrimSpace(text)
	if text == "" {
		return Commitment{}, invalidError("check-in text is required")
	}
	if quietPeriod <= 0 {
		return Commitment{}, invalidError("check-in quiet period must be positive")
	}
	return s.mutatePendingCommitment(ctx, userID, func(tx *sql.Tx, active Commitment, now time.Time) error {
		if !active.Deadline.After(now) {
			return invalidError("check-in is only allowed before the deadline")
		}
		quietUntil := now.Add(quietPeriod).UTC()
		return ensureMutationApplied(tx.ExecContext(ctx, updateCommitmentCheckInSQL, ts(now), text, ts(quietUntil), ts(now), active.ID))
	})
}

func (s *Service) SubmitProof(ctx context.Context, userID string, submission ProofSubmission) (Commitment, error) {
	ctx = contextOrBackground(ctx)
	submission.Text = strings.TrimSpace(submission.Text)
	submission.Verdict = strings.TrimSpace(submission.Verdict)
	meta, _ := json.Marshal(submission)
	return s.mutatePendingCommitment(ctx, userID, func(tx *sql.Tx, active Commitment, now time.Time) error {
		status := StatusSuccess
		if !active.Deadline.After(now) {
			status = StatusFailed
		}
		return ensureMutationApplied(tx.ExecContext(ctx, updateCommitmentProofSQL, string(status), string(meta), ts(now), active.ID))
	})
}

type commitmentQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (s *Service) mutatePendingCommitment(ctx context.Context, userID string, mutate func(tx *sql.Tx, active Commitment, now time.Time) error) (Commitment, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Commitment{}, err
	}
	defer func() { _ = tx.Rollback() }()

	active, err := activeCommitmentForUser(ctx, tx, userID)
	if err != nil {
		return Commitment{}, err
	}
	if err := mutate(tx, active, s.now()); err != nil {
		return Commitment{}, err
	}
	commitment, err := getCommitmentByID(ctx, tx, active.ID)
	if err != nil {
		return Commitment{}, err
	}
	if err := tx.Commit(); err != nil {
		return Commitment{}, err
	}
	return commitment, nil
}

func ensureMutationApplied(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected != 1 {
		return conflictError("active commitment changed before the update completed")
	}
	return nil
}

func getCommitmentByID(ctx context.Context, querier commitmentQuerier, id int64) (Commitment, error) {
	row := querier.QueryRowContext(contextOrBackground(ctx), commitmentSelect("WHERE id=? LIMIT 1;"), id)
	commitment, err := scanCommitment(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Commitment{}, notFoundError("commitment not found")
		}
		return Commitment{}, err
	}
	return commitment, nil
}

func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

type commitmentScanner interface {
	Scan(dest ...any) error
}

func scanCommitment(scanner commitmentScanner) (Commitment, error) {
	var commitment Commitment
	var createdAtRaw string
	var deadlineRaw string
	var snoozedUntilRaw string
	var lastCheckInAtRaw string
	var checkInQuietUntilRaw string
	var policyConfigRaw string
	var reminderCount int
	var statusRaw string
	var updatedAtRaw string

	if err := scanner.Scan(
		&commitment.ID,
		&commitment.UserID,
		&commitment.Task,
		&createdAtRaw,
		&deadlineRaw,
		&snoozedUntilRaw,
		&lastCheckInAtRaw,
		&commitment.LastCheckInText,
		&checkInQuietUntilRaw,
		&reminderCount,
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
	commitment.ReminderCount = reminderCount
	commitment.Status = Status(strings.TrimSpace(statusRaw))
	commitment.CreatedAt = parseTimestamp(createdAtRaw)
	commitment.Deadline = parseTimestamp(deadlineRaw)
	commitment.SnoozedUntil = parseTimestamp(snoozedUntilRaw)
	commitment.LastCheckInAt = parseTimestamp(lastCheckInAtRaw)
	commitment.CheckInQuietUntil = parseTimestamp(checkInQuietUntilRaw)
	commitment.UpdatedAt = parseTimestamp(updatedAtRaw)

	return commitment, nil
}

func parseTimestamp(raw string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw))
	return parsed
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
