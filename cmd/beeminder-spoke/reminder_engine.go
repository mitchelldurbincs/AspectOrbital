package main

import (
	"sync"
	"time"
)

const progressEpsilon = 1e-9

type dailySnapshot struct {
	CheckedAt      time.Time     `json:"checkedAt"`
	LocalNow       time.Time     `json:"localNow"`
	GoalSlug       string        `json:"goalSlug"`
	GoalTitle      string        `json:"goalTitle"`
	GoalUnits      string        `json:"goalUnits"`
	GoalAggDay     string        `json:"goalAggDay"`
	Daystamp       string        `json:"daystamp"`
	DailyTarget    float64       `json:"dailyTarget"`
	TodayProgress  float64       `json:"todayProgress"`
	ReminderStart  time.Time     `json:"reminderStart"`
	ReminderWindow time.Duration `json:"reminderWindow"`
}

type reminderState struct {
	Daystamp            string
	LastProgress        float64
	NextReminderAt      time.Time
	SnoozedUntil        time.Time
	ActiveUntil         time.Time
	LastReminderAt      time.Time
	LastReminderMessage string
	Completed           bool
}

type reminderEngine struct {
	mu           sync.Mutex
	cfg          config
	state        reminderState
	hasSnapshot  bool
	lastSnapshot dailySnapshot
}

func newReminderEngine(cfg config) *reminderEngine {
	return &reminderEngine{cfg: cfg}
}

func (e *reminderEngine) Evaluate(snapshot dailySnapshot) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.hasSnapshot = true
	e.lastSnapshot = snapshot

	if e.state.Daystamp != snapshot.Daystamp {
		e.state = reminderState{
			Daystamp:     snapshot.Daystamp,
			LastProgress: snapshot.TodayProgress,
		}
	}

	if snapshot.TodayProgress > e.state.LastProgress+progressEpsilon {
		e.state.ActiveUntil = snapshot.CheckedAt.Add(e.cfg.ActiveGrace)
	}
	e.state.LastProgress = snapshot.TodayProgress

	if snapshot.TodayProgress >= snapshot.DailyTarget-progressEpsilon {
		e.state.Completed = true
		e.state.NextReminderAt = time.Time{}
		return false
	}

	e.state.Completed = false

	if snapshot.CheckedAt.Before(snapshot.ReminderStart) {
		return false
	}

	if !e.state.SnoozedUntil.IsZero() && snapshot.CheckedAt.Before(e.state.SnoozedUntil) {
		return false
	}

	if !e.state.ActiveUntil.IsZero() && snapshot.CheckedAt.Before(e.state.ActiveUntil) {
		return false
	}

	if e.state.NextReminderAt.IsZero() {
		return true
	}

	return !snapshot.CheckedAt.Before(e.state.NextReminderAt)
}

func (e *reminderEngine) MarkReminderSent(now time.Time, message string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.state.LastReminderAt = now
	e.state.LastReminderMessage = message
	e.state.NextReminderAt = now.Add(e.cfg.ReminderInterval)
}

func (e *reminderEngine) Snooze(now time.Time, duration time.Duration) time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()

	until := now.Add(duration)
	if until.After(e.state.SnoozedUntil) {
		e.state.SnoozedUntil = until
	}
	e.state.NextReminderAt = e.state.SnoozedUntil

	return e.state.SnoozedUntil
}

func (e *reminderEngine) MarkStarted(now time.Time) time.Time {
	return e.Snooze(now, e.cfg.StartedSnooze)
}

func (e *reminderEngine) Resume(now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.state.SnoozedUntil = time.Time{}
	e.state.ActiveUntil = time.Time{}
	e.state.NextReminderAt = now
}

func (e *reminderEngine) Status() reminderStatus {
	e.mu.Lock()
	defer e.mu.Unlock()

	status := reminderStatus{
		Commands:            e.cfg.Commands,
		CurrentDaystamp:     e.state.Daystamp,
		Completed:           e.state.Completed,
		NextReminderAt:      optionalTime(e.state.NextReminderAt),
		SnoozedUntil:        optionalTime(e.state.SnoozedUntil),
		ActiveUntil:         optionalTime(e.state.ActiveUntil),
		LastReminderAt:      optionalTime(e.state.LastReminderAt),
		LastReminderMessage: e.state.LastReminderMessage,
	}

	if e.hasSnapshot {
		snapshot := e.lastSnapshot
		status.LastSnapshot = &snapshot
	}

	return status
}

type reminderStatus struct {
	Commands            controlCommands `json:"commands"`
	CurrentDaystamp     string          `json:"currentDaystamp"`
	Completed           bool            `json:"completed"`
	NextReminderAt      *time.Time      `json:"nextReminderAt,omitempty"`
	SnoozedUntil        *time.Time      `json:"snoozedUntil,omitempty"`
	ActiveUntil         *time.Time      `json:"activeUntil,omitempty"`
	LastReminderAt      *time.Time      `json:"lastReminderAt,omitempty"`
	LastReminderMessage string          `json:"lastReminderMessage,omitempty"`
	LastSnapshot        *dailySnapshot  `json:"lastSnapshot,omitempty"`
}

func optionalTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}

	copy := value
	return &copy
}
