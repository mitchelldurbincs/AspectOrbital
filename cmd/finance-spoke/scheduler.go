package main

import (
	"context"
	"fmt"
	"log"
	"time"
)

const failedRunRetryInterval = 6 * time.Hour

type scheduler struct {
	cfg      config
	log      *log.Logger
	hub      *hubClient
	plaid    *plaidClient
	state    *stateStore
	location *time.Location
}

func newScheduler(cfg config, logger *log.Logger, hub *hubClient, plaid *plaidClient, state *stateStore, location *time.Location) *scheduler {
	return &scheduler{
		cfg:      cfg,
		log:      logger,
		hub:      hub,
		plaid:    plaid,
		state:    state,
		location: location,
	}
}

func (s *scheduler) RunDue(ctx context.Context, now time.Time) error {
	if !s.cfg.SummaryEnabled {
		return nil
	}

	latestSchedule := s.latestScheduleAtOrBefore(now)
	weekKey := weekKeyForSchedule(latestSchedule)

	state := s.state.Snapshot()
	if state.LastSentWeekKey == weekKey {
		return nil
	}
	if state.LastRunWeekKey == weekKey && !state.LastRunSucceeded && !state.LastRunAt.IsZero() {
		nextRetryAt := state.LastRunAt.Add(failedRunRetryInterval)
		if now.UTC().Before(nextRetryAt) {
			return nil
		}
	}

	if now.Before(latestSchedule) {
		return nil
	}

	err := s.sendSummary(ctx, latestSchedule, weekKey)
	if markErr := s.state.MarkRun(weekKey, now.UTC(), err); markErr != nil {
		s.log.Printf("state update failed after summary run: %v", markErr)
	}

	return err
}

func (s *scheduler) RunNow(ctx context.Context, now time.Time) error {
	if !s.cfg.SummaryEnabled {
		return fmt.Errorf("summary is disabled (FINANCE_SUMMARY_ENABLED=false)")
	}

	latestSchedule := s.latestScheduleAtOrBefore(now)
	weekKey := weekKeyForSchedule(latestSchedule)
	err := s.sendSummary(ctx, latestSchedule, weekKey)
	if markErr := s.state.MarkRun(weekKey, now.UTC(), err); markErr != nil {
		s.log.Printf("state update failed after manual run: %v", markErr)
	}

	return err
}

func (s *scheduler) sendSummary(ctx context.Context, scheduledAt time.Time, weekKey string) error {
	windowEnd := scheduledAt
	windowStart := scheduledAt.AddDate(0, 0, -s.cfg.SummaryLookbackDays)

	charges, err := s.plaid.WeeklySubscriptions(ctx, windowStart, windowEnd, s.location)
	if err != nil {
		return err
	}

	summary := weeklySummary{
		WeekKey:     weekKey,
		WindowStart: windowStart,
		WindowEnd:   windowEnd,
		Charges:     charges,
	}

	total := 0.0
	for _, charge := range charges {
		total += charge.Amount
	}
	summary.TotalAmount = total

	if len(summary.Charges) == 0 && !s.cfg.SummarySendEmpty {
		s.log.Printf("skipping empty weekly summary for week %s", weekKey)
		return s.state.MarkSummarySent(weekKey, time.Now().UTC(), 0, 0)
	}

	message := renderWeeklySummaryMessage(s.cfg.SummaryTitle, summary, s.location, s.cfg.SummaryMaxItems)
	err = s.hub.Notify(ctx, hubNotifyRequest{
		TargetChannel: s.cfg.NotifyTargetChannel,
		Message:       message,
		Severity:      s.cfg.NotifySeverity,
	})
	if err != nil {
		return err
	}

	s.log.Printf("sent weekly subscription summary for week %s with %d item(s)", weekKey, len(summary.Charges))

	return s.state.MarkSummarySent(weekKey, time.Now().UTC(), len(summary.Charges), summary.TotalAmount)
}

func (s *scheduler) nextScheduleAfter(now time.Time) time.Time {
	latest := s.latestScheduleAtOrBefore(now)
	return latest.AddDate(0, 0, 7)
}

func (s *scheduler) latestScheduleAtOrBefore(now time.Time) time.Time {
	nowLocal := now.In(s.location)

	daysSinceScheduledWeekday := (int(nowLocal.Weekday()) - int(s.cfg.SummaryWeekday) + 7) % 7
	scheduledDate := nowLocal.AddDate(0, 0, -daysSinceScheduledWeekday)

	candidate := time.Date(
		scheduledDate.Year(),
		scheduledDate.Month(),
		scheduledDate.Day(),
		s.cfg.SummaryHour,
		s.cfg.SummaryMinute,
		0,
		0,
		s.location,
	)

	if nowLocal.Before(candidate) {
		candidate = candidate.AddDate(0, 0, -7)
	}

	return candidate
}

func weekKeyForSchedule(scheduledAt time.Time) string {
	return scheduledAt.Format("2006-01-02")
}
