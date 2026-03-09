package main

import (
	"math"
	"strings"
	"testing"
	"time"

	"personal-infrastructure/pkg/beeminder"
)

func TestDailyTargetForGoal(t *testing.T) {
	tests := []struct {
		name    string
		goal    beeminder.Goal
		want    float64
		wantErr bool
	}{
		{name: "daily", goal: beeminder.Goal{Rate: ptr(2.5), Runits: "d"}, want: 2.5},
		{name: "weekly", goal: beeminder.Goal{Rate: ptr(7), Runits: "w"}, want: 1},
		{name: "monthly", goal: beeminder.Goal{Rate: ptr(30), Runits: "m"}, want: 1},
		{name: "yearly", goal: beeminder.Goal{Rate: ptr(365), Runits: "y"}, want: 1},
		{name: "hourly", goal: beeminder.Goal{Rate: ptr(1), Runits: "h"}, want: 24},
		{name: "missing rate", goal: beeminder.Goal{Runits: "d"}, wantErr: true},
		{name: "invalid runits", goal: beeminder.Goal{Rate: ptr(1), Runits: "q"}, wantErr: true},
		{name: "non-positive rate", goal: beeminder.Goal{Rate: ptr(0), Runits: "d"}, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := dailyTargetForGoal(tc.goal)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if math.Abs(got-tc.want) > 1e-9 {
				t.Fatalf("dailyTargetForGoal() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAggregateDayProgress(t *testing.T) {
	datapoints := []beeminder.Datapoint{{Value: 2, Timestamp: 100}, {Value: 10, Timestamp: 50, IsDummy: true}, {Value: 4, Timestamp: 200}, {Value: 3, Timestamp: 150}}
	tests := []struct {
		name   string
		aggDay string
		want   float64
	}{
		{name: "sum default", aggDay: "sum", want: 9},
		{name: "max", aggDay: "max", want: 4},
		{name: "min", aggDay: "min", want: 2},
		{name: "mean", aggDay: "mean", want: 3},
		{name: "last", aggDay: "last", want: 4},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := aggregateDayProgress(datapoints, tc.aggDay)
			if got != tc.want {
				t.Fatalf("aggregateDayProgress() = %v, want %v", got, tc.want)
			}
		})
	}

	if got := aggregateDayProgress([]beeminder.Datapoint{{IsDummy: true}}, "max"); got != 0 {
		t.Fatalf("aggregateDayProgress() with only dummies = %v, want 0", got)
	}
}

func TestReminderStartForDay(t *testing.T) {
	loc := time.FixedZone("UTC-5", -5*60*60)
	now := time.Date(2026, 1, 2, 20, 30, 40, 0, loc)
	got := reminderStartForDay(now, 9, 15)
	want := time.Date(2026, 1, 2, 9, 15, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("reminderStartForDay() = %v, want %v", got, want)
	}
}

func ptr(v float64) *float64 { return &v }

func TestRenderReminderMessageAndHelpers(t *testing.T) {
	snapshot := dailySnapshot{GoalSlug: "focus", GoalUnits: "hours", TodayProgress: 0.5, DailyTarget: 1.0, ReminderWindow: 5 * time.Minute}
	message := renderReminderMessage(snapshot)
	if !strings.Contains(message, "30.0/60.0 minutes") || !strings.Contains(message, "5m") {
		t.Fatalf("unexpected reminder message: %q", message)
	}

	if got := beeminderDaystamp(time.Date(2026, 1, 2, 0, 30, 0, 0, time.UTC), 3600); got != "20260101" {
		t.Fatalf("beeminderDaystamp() = %q, want 20260101", got)
	}

	if !looksLikeHours("hrs") || looksLikeHours("pages") {
		t.Fatalf("looksLikeHours() produced unexpected result")
	}

	if got := humanDuration(90 * time.Second); got != "1m30s" {
		t.Fatalf("humanDuration() = %q, want 1m30s", got)
	}
}
