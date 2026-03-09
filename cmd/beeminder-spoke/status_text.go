package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

func summarizeStatus(status reminderStatus, location *time.Location) string {
	if len(status.Goals) == 0 {
		return "No goals configured."
	}

	goalSlugs := make([]string, 0, len(status.Goals))
	for goalSlug := range status.Goals {
		goalSlugs = append(goalSlugs, goalSlug)
	}
	sort.Strings(goalSlugs)

	parts := make([]string, 0, len(goalSlugs))
	for _, goalSlug := range goalSlugs {
		goalStatus := status.Goals[goalSlug]
		parts = append(parts, summarizeGoalStatus(goalSlug, goalStatus, location))
	}

	return strings.Join(parts, " | ")
}

func summarizeGoalStatus(goalSlug string, status goalReminderStatus, location *time.Location) string {
	if status.Completed {
		return fmt.Sprintf("%s: complete", goalSlug)
	}

	if status.LastSnapshot == nil {
		if status.SnoozedUntil != nil {
			return fmt.Sprintf("%s: waiting (snoozed until %s)", goalSlug, formatClockInLocation(*status.SnoozedUntil, location))
		}

		return fmt.Sprintf("%s: waiting for first snapshot", goalSlug)
	}

	done := status.LastSnapshot.TodayProgress
	target := status.LastSnapshot.RequiredProgress
	remaining := target - done
	if remaining < 0 {
		remaining = 0
	}

	unit := strings.TrimSpace(status.LastSnapshot.GoalUnits)
	if unit == "" {
		unit = "units"
	}
	decimals := 2
	if looksLikeHours(unit) {
		done *= 60
		target *= 60
		remaining *= 60
		unit = "min"
		decimals = 1
	}

	message := fmt.Sprintf(
		"%s: %s/%s %s done, %s %s left",
		goalSlug,
		formatFloat(done, decimals),
		formatFloat(target, decimals),
		unit,
		formatFloat(remaining, decimals),
		unit,
	)

	nextPingAt := resolveNextPingTime(status)
	if !nextPingAt.IsZero() {
		message += fmt.Sprintf(" (next ping %s)", formatClockInLocation(nextPingAt, location))
	}

	return message
}

func resolveNextPingTime(status goalReminderStatus) time.Time {
	if status.SnoozedUntil != nil {
		return *status.SnoozedUntil
	}

	if status.NextReminderAt != nil {
		return *status.NextReminderAt
	}

	if status.LastSnapshot == nil {
		return time.Time{}
	}

	if status.LastSnapshot.LocalNow.Before(status.LastSnapshot.ReminderStart) {
		return status.LastSnapshot.ReminderStart
	}

	if status.LastReminderAt != nil && status.LastSnapshot.ReminderWindow > 0 {
		return status.LastReminderAt.Add(status.LastSnapshot.ReminderWindow)
	}

	return status.LastSnapshot.LocalNow
}

func formatClockInLocation(value time.Time, location *time.Location) string {
	if location != nil {
		value = value.In(location)
	} else {
		value = value.Local()
	}

	return value.Format("3:04 PM MST")
}

func formatFloat(value float64, decimals int) string {
	return strconv.FormatFloat(value, 'f', decimals, 64)
}
