package main

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"personal-infrastructure/pkg/beeminder"
)

const daystampFormat = "20060102"

func beeminderDaystamp(now time.Time, deadlineSeconds int) string {
	shifted := now.Add(-time.Duration(deadlineSeconds) * time.Second)
	return shifted.Format(daystampFormat)
}

func dailyTargetForGoal(goal beeminder.Goal) (float64, error) {
	if goal.Rate == nil {
		return 0, errors.New("beeminder goal does not provide a usable rate")
	}

	rate := *goal.Rate
	if rate <= 0 {
		return 0, fmt.Errorf("unsupported goal rate: %.3f", rate)
	}

	switch strings.ToLower(strings.TrimSpace(goal.Runits)) {
	case "d":
		return rate, nil
	case "w":
		return rate / 7.0, nil
	case "m":
		return rate / 30.0, nil
	case "y":
		return rate / 365.0, nil
	case "h":
		return rate * 24.0, nil
	default:
		return 0, fmt.Errorf("unsupported runits %q; expected one of d,w,m,y,h", goal.Runits)
	}
}

func requiredProgressForGoal(goal beeminder.Goal, requireDailyRate bool) (float64, error) {
	roadDue := 0.0
	if goal.Delta != nil && *goal.Delta > 0 {
		roadDue = *goal.Delta
	}

	if !requireDailyRate {
		return roadDue, nil
	}

	dailyRate, err := dailyTargetForGoal(goal)
	if err != nil {
		if goal.Delta != nil {
			return roadDue, nil
		}
		return 0, err
	}

	if dailyRate > roadDue {
		return dailyRate, nil
	}

	return roadDue, nil
}

func aggregateDayProgress(datapoints []beeminder.Datapoint, aggDay string) float64 {
	clean := make([]beeminder.Datapoint, 0, len(datapoints))
	for _, datapoint := range datapoints {
		if datapoint.IsDummy {
			continue
		}
		clean = append(clean, datapoint)
	}

	if len(clean) == 0 {
		return 0
	}

	switch strings.ToLower(strings.TrimSpace(aggDay)) {
	case "max":
		maxValue := clean[0].Value
		for i := 1; i < len(clean); i++ {
			if clean[i].Value > maxValue {
				maxValue = clean[i].Value
			}
		}
		return maxValue
	case "min":
		minValue := clean[0].Value
		for i := 1; i < len(clean); i++ {
			if clean[i].Value < minValue {
				minValue = clean[i].Value
			}
		}
		return minValue
	case "mean":
		total := 0.0
		for _, datapoint := range clean {
			total += datapoint.Value
		}
		return total / float64(len(clean))
	case "last":
		latest := clean[0]
		for i := 1; i < len(clean); i++ {
			if clean[i].Timestamp > latest.Timestamp {
				latest = clean[i]
			}
		}
		return latest.Value
	default:
		total := 0.0
		for _, datapoint := range clean {
			total += datapoint.Value
		}
		return total
	}
}

func reminderStartForDay(now time.Time, hour, minute int) time.Time {
	return time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
}

func renderReminderMessage(snapshot dailySnapshot) string {
	remaining := snapshot.RequiredProgress - snapshot.TodayProgress
	if remaining < 0 {
		remaining = 0
	}

	goalLabel := snapshot.GoalSlug
	if goalLabel == "" {
		goalLabel = "beeminder"
	}

	units := strings.TrimSpace(snapshot.GoalUnits)
	if units == "" {
		units = "units"
	}

	base := fmt.Sprintf(
		"%s reminder: %.2f/%.2f %s done today (%.2f left).",
		goalLabel,
		snapshot.TodayProgress,
		snapshot.RequiredProgress,
		units,
		remaining,
	)

	if looksLikeHours(units) {
		base = fmt.Sprintf(
			"%s reminder: %.1f/%.1f minutes done today (%.1f min left).",
			goalLabel,
			snapshot.TodayProgress*60,
			snapshot.RequiredProgress*60,
			remaining*60,
		)
	}

	if snapshot.ReminderWindow > 0 {
		base += fmt.Sprintf(" I will ping again in %s.", humanDuration(snapshot.ReminderWindow))
	}

	if strings.TrimSpace(snapshot.ActionURL) != "" {
		base += fmt.Sprintf(" Start now: %s", snapshot.ActionURL)
	}

	return base
}

func looksLikeHours(units string) bool {
	normalized := strings.ToLower(strings.TrimSpace(units))
	return strings.Contains(normalized, "hour") || normalized == "hr" || normalized == "hrs" || normalized == "h"
}

func humanDuration(duration time.Duration) string {
	if duration <= 0 {
		return duration.String()
	}

	if duration < time.Minute {
		return duration.Round(time.Second).String()
	}

	minutes := duration.Minutes()
	if math.Mod(minutes, 1.0) == 0 {
		return fmt.Sprintf("%.0fm", minutes)
	}

	return duration.Round(time.Second).String()
}
