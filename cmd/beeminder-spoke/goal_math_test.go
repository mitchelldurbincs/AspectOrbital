package main

import (
	"strings"
	"testing"
	"time"

	"personal-infrastructure/pkg/beeminder"
)

func TestRequiredProgressForGoalUsesMaxOfDeltaAndDailyRate(t *testing.T) {
	rate := 2.0
	delta := 1.0
	goal := beeminder.Goal{Rate: &rate, Runits: "d", Delta: &delta}

	got, err := requiredProgressForGoal(goal, true)
	if err != nil {
		t.Fatalf("requiredProgressForGoal() error = %v", err)
	}
	if got != 2.0 {
		t.Fatalf("requiredProgressForGoal() = %.2f, want 2.00", got)
	}
}

func TestRequiredProgressForGoalFallsBackToDeltaWhenRateInvalid(t *testing.T) {
	rate := 3.0
	delta := 1.5
	goal := beeminder.Goal{Rate: &rate, Runits: "bad", Delta: &delta}

	got, err := requiredProgressForGoal(goal, true)
	if err != nil {
		t.Fatalf("requiredProgressForGoal() error = %v", err)
	}
	if got != 1.5 {
		t.Fatalf("requiredProgressForGoal() = %.2f, want 1.50", got)
	}
}

func TestAggregateDayProgressIgnoresDummyAndUsesLast(t *testing.T) {
	datapoints := []beeminder.Datapoint{
		{Value: 100, Timestamp: 1000, IsDummy: true},
		{Value: 2, Timestamp: 2000},
		{Value: 7, Timestamp: 3000},
	}

	got := aggregateDayProgress(datapoints, "last")
	if got != 7 {
		t.Fatalf("aggregateDayProgress() = %.2f, want 7.00", got)
	}
}

func TestRenderReminderMessageIncludesScheduleAndActionURL(t *testing.T) {
	message := renderReminderMessage(dailySnapshot{
		GoalSlug:         "skritter",
		GoalUnits:        "hours",
		TodayProgress:    1,
		RequiredProgress: 2,
		ReminderWindow:   30 * time.Minute,
		ActionURL:        "https://example.com/start",
	})

	if !strings.Contains(message, "60.0/120.0 minutes done today") {
		t.Fatalf("message missing hour-to-minute conversion: %q", message)
	}
	if !strings.Contains(message, "I will ping again in 30m") {
		t.Fatalf("message missing next ping hint: %q", message)
	}
	if !strings.Contains(message, "Start now: https://example.com/start") {
		t.Fatalf("message missing action URL: %q", message)
	}
}
