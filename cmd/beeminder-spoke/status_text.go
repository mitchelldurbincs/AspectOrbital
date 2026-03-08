package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func summarizeStatus(status reminderStatus, location *time.Location) string {
	if status.Completed {
		return "Goal complete for today. No reminders are active."
	}

	if status.LastSnapshot == nil {
		if status.SnoozedUntil != nil {
			return fmt.Sprintf("No snapshot yet. Reminders are snoozed until %s.", formatClockInLocation(*status.SnoozedUntil, location))
		}

		return "No Beeminder snapshot yet."
	}

	done := status.LastSnapshot.TodayProgress
	target := status.LastSnapshot.DailyTarget
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
		"Today: %s/%s %s, %s %s left.",
		formatFloat(done, decimals),
		formatFloat(target, decimals),
		unit,
		formatFloat(remaining, decimals),
		unit,
	)

	nextPingAt := resolveNextPingTime(status)
	if !nextPingAt.IsZero() {
		message += fmt.Sprintf(" Next ping: %s.", formatClockInLocation(nextPingAt, location))
	}

	return message
}

func resolveNextPingTime(status reminderStatus) time.Time {
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
