package main

import (
	"context"
	"time"

	"personal-infrastructure/pkg/hubnotify"
)

func (a *spokeApp) runCycle(parentCtx context.Context) error {
	ctx, cancel := context.WithTimeout(parentCtx, cycleTimeout)
	defer cancel()

	nowUTC := time.Now().UTC()
	nowLocal := nowUTC.In(a.location)

	goal, err := a.beeminder.GetGoal(ctx, a.cfg.BeeminderGoalSlug)
	if err != nil {
		return err
	}

	target, err := dailyTargetForGoal(goal)
	if err != nil {
		return err
	}

	daystamp := beeminderDaystamp(nowLocal, goal.Deadline)
	datapoints, err := a.beeminder.GetDatapointsForDay(ctx, a.cfg.BeeminderGoalSlug, daystamp)
	if err != nil {
		return err
	}

	progress := aggregateDayProgress(datapoints, goal.AggDay)
	reminderStart := reminderStartForDay(nowLocal, a.cfg.ReminderStartHour, a.cfg.ReminderStartMinute)

	snapshot := dailySnapshot{
		CheckedAt:      nowUTC,
		LocalNow:       nowLocal,
		GoalSlug:       goal.Slug,
		GoalTitle:      goal.Title,
		GoalUnits:      goal.GUnits,
		GoalAggDay:     goal.AggDay,
		Daystamp:       daystamp,
		DailyTarget:    target,
		TodayProgress:  progress,
		ReminderStart:  reminderStart,
		ReminderWindow: a.cfg.ReminderInterval,
	}

	if !a.engine.Evaluate(snapshot) {
		return nil
	}

	message := renderReminderMessage(snapshot)
	if err := a.hub.Notify(ctx, hubnotify.NotifyRequest{
		TargetChannel: a.cfg.NotifyTargetChannel,
		Message:       message,
		Severity:      a.cfg.NotifySeverity,
	}); err != nil {
		return err
	}

	a.engine.MarkReminderSent(nowUTC, message)
	a.log.Printf("sent reminder for day %s: %.3f/%.3f %s", snapshot.Daystamp, snapshot.TodayProgress, snapshot.DailyTarget, snapshot.GoalUnits)

	return nil
}
