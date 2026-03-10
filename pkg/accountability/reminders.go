package accountability

import (
	"context"
	"errors"
	"time"
)

func (s *Service) ExpireOverdue(ctx context.Context) (int64, error) {
	ctx = contextOrBackground(ctx)
	now := s.now()
	cutoff := now.Add(-s.expiryGrace)
	result, err := s.db.ExecContext(ctx, updateOverdueCommitmentsSQL, ts(now), ts(cutoff))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Service) OverdueNeedingReminder(ctx context.Context, reminderInterval time.Duration) ([]Commitment, error) {
	if reminderInterval <= 0 {
		return nil, errors.New("reminder interval must be positive")
	}
	ctx = contextOrBackground(ctx)
	now := s.now()
	earliestReminder := now.Add(-reminderInterval)

	var rows commitmentRows
	var err error
	if s.expiryGrace > 0 {
		rows, err = s.db.QueryContext(
			ctx,
			overdueNeedingReminderQuery(true),
			ts(now),
			ts(now.Add(-s.expiryGrace)),
			ts(now),
			ts(now),
			ts(earliestReminder),
		)
	} else {
		rows, err = s.db.QueryContext(
			ctx,
			overdueNeedingReminderQuery(false),
			ts(now),
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

func (s *Service) MarkReminderSent(ctx context.Context, commitmentID int64) error {
	if commitmentID <= 0 {
		return errors.New("commitment id must be positive")
	}
	ctx = contextOrBackground(ctx)
	_, err := s.db.ExecContext(ctx, updateCommitmentReminderSQL, ts(s.now()), commitmentID)
	return err
}

type commitmentRows interface {
	Close() error
	Err() error
	Next() bool
	Scan(dest ...any) error
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
