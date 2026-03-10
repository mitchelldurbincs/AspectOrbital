package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"personal-infrastructure/pkg/hubnotify"
)

const (
	beeminderNotifyService = "beeminder-spoke"
	beeminderNotifyEvent   = "goal-reminder"
)

func (a *spokeApp) runCycle(parentCtx context.Context) error {
	ctx, cancel := context.WithTimeout(parentCtx, cycleTimeout)
	defer cancel()

	nowUTC := time.Now().UTC()
	nowLocal := nowUTC.In(a.location)

	var errs []error
	for _, goalSlug := range a.cfg.BeeminderGoalSlugs {
		if err := a.runGoalCycle(ctx, goalSlug, nowUTC, nowLocal); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

func (a *spokeApp) runGoalCycle(ctx context.Context, goalSlug string, nowUTC, nowLocal time.Time) error {
	goal, err := a.beeminder.GetGoal(ctx, goalSlug)
	if err != nil {
		return fmt.Errorf("goal %q: %w", goalSlug, err)
	}

	requiredProgress, err := requiredProgressForGoal(goal, a.cfg.RequireDailyRate)
	if err != nil {
		return fmt.Errorf("goal %q: %w", goalSlug, err)
	}

	daystamp := beeminderDaystamp(nowLocal, goal.Deadline)
	datapoints, err := a.beeminder.GetDatapointsForDay(ctx, goalSlug, daystamp)
	if err != nil {
		return fmt.Errorf("goal %q: %w", goalSlug, err)
	}

	progress := aggregateDayProgress(datapoints, goal.AggDay)
	reminderStart := reminderStartForDay(nowLocal, a.cfg.ReminderStartHour, a.cfg.ReminderStartMinute)
	reminderWindow := reminderIntervalForLocalTime(nowLocal, a.cfg)

	roadDue := 0.0
	if goal.Delta != nil && *goal.Delta > 0 {
		roadDue = *goal.Delta
	}

	safeBufferDays := 0.0
	if goal.SafeBuf != nil {
		safeBufferDays = *goal.SafeBuf
	}

	snapshot := dailySnapshot{
		CheckedAt:         nowUTC,
		LocalNow:          nowLocal,
		GoalSlug:          goal.Slug,
		GoalTitle:         goal.Title,
		GoalUnits:         goal.GUnits,
		GoalAggDay:        goal.AggDay,
		Daystamp:          daystamp,
		RequiredProgress:  requiredProgress,
		RoadDue:           roadDue,
		SafeBufferDays:    safeBufferDays,
		TodayProgress:     progress,
		ReminderStart:     reminderStart,
		ReminderWindow:    reminderWindow,
		ActionURL:         a.cfg.ActionURLs[goalSlug],
		RequireDailyRate:  a.cfg.RequireDailyRate,
		HasBedtime:        a.cfg.HasBedtime,
		ConfiguredBedtime: configuredBedtimeLabel(a.cfg),
	}

	if !a.engine.Evaluate(goalSlug, snapshot) {
		return nil
	}

	message := renderReminderMessage(snapshot)
	if err := a.hub.Notify(ctx, hubnotify.NotifyRequest{
		Version:       hubnotify.Version2,
		TargetChannel: a.cfg.NotifyTargetChannel,
		Service:       beeminderNotifyService,
		Event:         beeminderNotifyEvent,
		Severity:      a.cfg.NotifySeverity,
		Title:         hubnotify.CanonicalTitle(beeminderNotifyService, beeminderNotifyEvent),
		Summary:       message,
		URL:           snapshot.ActionURL,
		Fields: []hubnotify.NotifyField{
			{Key: "Goal", Value: snapshot.GoalSlug, Group: hubnotify.FieldGroupContext, Order: 10, Inline: true},
			{Key: "Progress", Value: fmt.Sprintf("%.3f / %.3f %s", snapshot.TodayProgress, snapshot.RequiredProgress, snapshot.GoalUnits), Group: hubnotify.FieldGroupMetrics, Order: 20, Inline: true},
			{Key: "Checked At", Value: snapshot.CheckedAt.UTC().Format(time.RFC3339), Group: hubnotify.FieldGroupTiming, Order: 30, Inline: false},
		},
		CallbackURL: a.cfg.DiscordCallbackURL,
		Actions: []hubnotify.NotifyAction{
			{ID: scopedDiscordActionID(discordActionSnooze10m, goalSlug), Label: "Snooze 10m", Style: hubnotify.ActionStyleSecondary},
			{ID: scopedDiscordActionID(discordActionSnooze30m, goalSlug), Label: "Snooze 30m", Style: hubnotify.ActionStyleSecondary},
			{ID: scopedDiscordActionID(discordActionAcknowledge, goalSlug), Label: "Acknowledge", Style: hubnotify.ActionStyleSuccess},
		},
		AllowedMentions:       hubnotify.AllowedMentions{Parse: []string{}, Users: []string{}, Roles: []string{}, RepliedUser: false},
		Visibility:            hubnotify.VisibilityPublic,
		SuppressNotifications: false,
		OccurredAt:            snapshot.CheckedAt.UTC(),
	}); err != nil {
		return fmt.Errorf("goal %q: %w", goalSlug, err)
	}

	a.engine.MarkReminderSent(goalSlug, nowUTC, nowLocal, message)
	a.log.Printf("sent reminder for goal %q day %s: %.3f/%.3f %s", goalSlug, snapshot.Daystamp, snapshot.TodayProgress, snapshot.RequiredProgress, snapshot.GoalUnits)

	return nil
}

func scopedDiscordActionID(actionID string, goalSlug string) string {
	return actionID + ":" + goalSlug
}

func configuredBedtimeLabel(cfg config) string {
	if !cfg.HasBedtime {
		return ""
	}

	return fmt.Sprintf("%02d:%02d", cfg.BedtimeHour, cfg.BedtimeMinute)
}
