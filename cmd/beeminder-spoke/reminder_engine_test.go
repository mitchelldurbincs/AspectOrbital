package main

import (
	"testing"
	"time"
)

func TestReminderEngineTimingRules(t *testing.T) {
	cfg := config{ReminderInterval: 30 * time.Minute, ActiveGrace: 10 * time.Minute, StartedSnooze: 1 * time.Hour}
	engine := newReminderEngine(cfg)

	base := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)
	start := base.Add(30 * time.Minute)
	snapshot := dailySnapshot{Daystamp: "20260105", DailyTarget: 10, TodayProgress: 2, ReminderStart: start}

	checks := []struct {
		name string
		now  time.Time
		mut  func()
		want bool
	}{
		{name: "before reminder start", now: start.Add(-time.Minute), want: false},
		{name: "at reminder start first reminder", now: start, want: true},
		{name: "after mark sent before interval", now: start.Add(10 * time.Minute), mut: func() { engine.MarkReminderSent(start, "ping") }, want: false},
		{name: "at next interval", now: start.Add(30 * time.Minute), want: true},
		{name: "progress increase activates grace", now: start.Add(31 * time.Minute), mut: func() { snapshot.TodayProgress = 3 }, want: false},
		{name: "after active grace expires", now: start.Add(42 * time.Minute), want: true},
		{name: "during snooze", now: start.Add(45 * time.Minute), mut: func() { engine.Snooze(start.Add(40*time.Minute), 15*time.Minute) }, want: false},
		{name: "after resume", now: start.Add(46 * time.Minute), mut: func() { engine.Resume(start.Add(46 * time.Minute)) }, want: true},
		{name: "completed goal", now: start.Add(47 * time.Minute), mut: func() { snapshot.TodayProgress = snapshot.DailyTarget }, want: false},
		{name: "new day resets completion", now: start.Add(24*time.Hour + time.Minute), mut: func() {
			snapshot.Daystamp = "20260106"
			snapshot.TodayProgress = 0
			snapshot.ReminderStart = start.Add(24 * time.Hour)
		}, want: true},
	}

	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			if tc.mut != nil {
				tc.mut()
			}
			snapshot.CheckedAt = tc.now
			if got := engine.Evaluate(snapshot); got != tc.want {
				t.Fatalf("Evaluate() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestReminderEngineStatusAndOptionalTime(t *testing.T) {
	cfg := config{Commands: controlCommands{Snooze: "s", Resume: "r"}, ReminderInterval: time.Minute}
	engine := newReminderEngine(cfg)
	now := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)
	snap := dailySnapshot{CheckedAt: now, Daystamp: "20260105", DailyTarget: 2, TodayProgress: 0, ReminderStart: now}
	_ = engine.Evaluate(snap)
	engine.MarkReminderSent(now, "hello")

	status := engine.Status()
	if status.LastSnapshot == nil || status.LastReminderMessage != "hello" || status.NextReminderAt == nil {
		t.Fatalf("unexpected status: %+v", status)
	}
	if optionalTime(time.Time{}) != nil {
		t.Fatalf("optionalTime(zero) should be nil")
	}
}
