package main

import (
	"testing"
	"time"
)

func TestEvaluateReturnsFalseBeforeReminderStart(t *testing.T) {
	cfg := testConfig()
	engine := newReminderEngine(cfg)
	now := time.Date(2026, time.March, 9, 8, 0, 0, 0, time.UTC)

	shouldSend := engine.Evaluate("study", dailySnapshot{
		CheckedAt:         now,
		LocalNow:          now,
		Daystamp:          "20260309",
		TodayProgress:     0,
		RequiredProgress:  1,
		ReminderStart:     time.Date(2026, time.March, 9, 9, 0, 0, 0, time.UTC),
		ReminderWindow:    30 * time.Minute,
		RequireDailyRate:  true,
		HasBedtime:        false,
		ConfiguredBedtime: "",
	})

	if shouldSend {
		t.Fatal("Evaluate() = true, want false before reminder start")
	}
}

func TestEvaluateSuppressesDuringActiveGraceAfterProgressIncrease(t *testing.T) {
	cfg := testConfig()
	cfg.ActiveGrace = 20 * time.Minute
	engine := newReminderEngine(cfg)
	start := time.Date(2026, time.March, 9, 10, 0, 0, 0, time.UTC)

	_ = engine.Evaluate("study", dailySnapshot{
		CheckedAt:        start,
		LocalNow:         start,
		Daystamp:         "20260309",
		TodayProgress:    0,
		RequiredProgress: 2,
		ReminderStart:    time.Date(2026, time.March, 9, 9, 0, 0, 0, time.UTC),
	})

	shouldSend := engine.Evaluate("study", dailySnapshot{
		CheckedAt:        start.Add(5 * time.Minute),
		LocalNow:         start.Add(5 * time.Minute),
		Daystamp:         "20260309",
		TodayProgress:    1,
		RequiredProgress: 2,
		ReminderStart:    time.Date(2026, time.March, 9, 9, 0, 0, 0, time.UTC),
	})

	if shouldSend {
		t.Fatal("Evaluate() = true, want false during active grace")
	}
}

func TestEvaluateSuppressesAtOrAfterBedtime(t *testing.T) {
	cfg := testConfig()
	cfg.HasBedtime = true
	cfg.BedtimeHour = 22
	cfg.BedtimeMinute = 0
	engine := newReminderEngine(cfg)
	now := time.Date(2026, time.March, 9, 22, 0, 0, 0, time.UTC)

	shouldSend := engine.Evaluate("study", dailySnapshot{
		CheckedAt:        now,
		LocalNow:         now,
		Daystamp:         "20260309",
		TodayProgress:    0,
		RequiredProgress: 1,
		ReminderStart:    time.Date(2026, time.March, 9, 9, 0, 0, 0, time.UTC),
	})

	if shouldSend {
		t.Fatal("Evaluate() = true, want false at bedtime boundary")
	}
}

func TestMarkReminderSentSetsNextReminderUsingInterval(t *testing.T) {
	cfg := testConfig()
	cfg.ReminderInterval = 45 * time.Minute
	engine := newReminderEngine(cfg)
	now := time.Date(2026, time.March, 9, 12, 0, 0, 0, time.UTC)

	engine.MarkReminderSent("study", now, now, "msg")
	status := engine.Status()
	goal := status.Goals["study"]
	if goal.NextReminderAt == nil {
		t.Fatal("NextReminderAt is nil")
	}

	want := now.Add(45 * time.Minute)
	if !goal.NextReminderAt.Equal(want) {
		t.Fatalf("NextReminderAt = %s, want %s", goal.NextReminderAt, want)
	}
}
