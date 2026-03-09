package main

import (
	"sync"
	"time"
)

const progressEpsilon = 1e-9

type dailySnapshot struct {
	CheckedAt         time.Time     `json:"checkedAt"`
	LocalNow          time.Time     `json:"localNow"`
	GoalSlug          string        `json:"goalSlug"`
	GoalTitle         string        `json:"goalTitle"`
	GoalUnits         string        `json:"goalUnits"`
	GoalAggDay        string        `json:"goalAggDay"`
	Daystamp          string        `json:"daystamp"`
	RequiredProgress  float64       `json:"requiredProgress"`
	RoadDue           float64       `json:"roadDue"`
	SafeBufferDays    float64       `json:"safeBufferDays"`
	TodayProgress     float64       `json:"todayProgress"`
	ReminderStart     time.Time     `json:"reminderStart"`
	ReminderWindow    time.Duration `json:"reminderWindow"`
	ActionURL         string        `json:"actionUrl,omitempty"`
	RequireDailyRate  bool          `json:"requireDailyRate"`
	HasBedtime        bool          `json:"hasBedtime"`
	ConfiguredBedtime string        `json:"configuredBedtime,omitempty"`
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
	mu            sync.Mutex
	cfg           config
	states        map[string]*reminderState
	hasSnapshot   map[string]bool
	lastSnapshots map[string]dailySnapshot
}

func newReminderEngine(cfg config) *reminderEngine {
	return &reminderEngine{
		cfg:           cfg,
		states:        make(map[string]*reminderState),
		hasSnapshot:   make(map[string]bool),
		lastSnapshots: make(map[string]dailySnapshot),
	}
}

func (e *reminderEngine) Evaluate(goalSlug string, snapshot dailySnapshot) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	state := e.stateForGoal(goalSlug)
	e.hasSnapshot[goalSlug] = true
	e.lastSnapshots[goalSlug] = snapshot

	if state.Daystamp != snapshot.Daystamp {
		*state = reminderState{
			Daystamp:     snapshot.Daystamp,
			LastProgress: snapshot.TodayProgress,
		}
	}

	if snapshot.TodayProgress > state.LastProgress+progressEpsilon {
		state.ActiveUntil = snapshot.CheckedAt.Add(e.cfg.ActiveGrace)
	}
	state.LastProgress = snapshot.TodayProgress

	if snapshot.RequiredProgress <= progressEpsilon || snapshot.TodayProgress >= snapshot.RequiredProgress-progressEpsilon {
		state.Completed = true
		state.NextReminderAt = time.Time{}
		return false
	}

	state.Completed = false

	if snapshot.CheckedAt.Before(snapshot.ReminderStart) {
		return false
	}

	if e.cfg.HasBedtime && isAtOrAfterBedtime(snapshot.LocalNow, e.cfg.BedtimeHour, e.cfg.BedtimeMinute) {
		return false
	}

	if !state.SnoozedUntil.IsZero() && snapshot.CheckedAt.Before(state.SnoozedUntil) {
		return false
	}

	if !state.ActiveUntil.IsZero() && snapshot.CheckedAt.Before(state.ActiveUntil) {
		return false
	}

	if state.NextReminderAt.IsZero() {
		return true
	}

	return !snapshot.CheckedAt.Before(state.NextReminderAt)
}

func (e *reminderEngine) MarkReminderSent(goalSlug string, nowUTC, localNow time.Time, message string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	state := e.stateForGoal(goalSlug)
	state.LastReminderAt = nowUTC
	state.LastReminderMessage = message
	state.NextReminderAt = nowUTC.Add(reminderIntervalForLocalTime(localNow, e.cfg))
}

func (e *reminderEngine) Snooze(now time.Time, duration time.Duration) time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()

	until := now.Add(duration)
	for _, goalSlug := range e.cfg.BeeminderGoalSlugs {
		state := e.stateForGoal(goalSlug)
		if until.After(state.SnoozedUntil) {
			state.SnoozedUntil = until
		}
		state.NextReminderAt = state.SnoozedUntil
	}

	return until
}

func (e *reminderEngine) MarkStarted(now time.Time) time.Time {
	return e.Snooze(now, e.cfg.StartedSnooze)
}

func (e *reminderEngine) Resume(now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, goalSlug := range e.cfg.BeeminderGoalSlugs {
		state := e.stateForGoal(goalSlug)
		state.SnoozedUntil = time.Time{}
		state.ActiveUntil = time.Time{}
		state.NextReminderAt = now
	}
}

func (e *reminderEngine) Status() reminderStatus {
	e.mu.Lock()
	defer e.mu.Unlock()

	status := reminderStatus{
		Commands: e.cfg.Commands,
		Goals:    make(map[string]goalReminderStatus, len(e.cfg.BeeminderGoalSlugs)),
	}

	for _, goalSlug := range e.cfg.BeeminderGoalSlugs {
		state := e.stateForGoal(goalSlug)
		goalStatus := goalReminderStatus{
			CurrentDaystamp:     state.Daystamp,
			Completed:           state.Completed,
			NextReminderAt:      optionalTime(state.NextReminderAt),
			SnoozedUntil:        optionalTime(state.SnoozedUntil),
			ActiveUntil:         optionalTime(state.ActiveUntil),
			LastReminderAt:      optionalTime(state.LastReminderAt),
			LastReminderMessage: state.LastReminderMessage,
		}

		if e.hasSnapshot[goalSlug] {
			snapshot := e.lastSnapshots[goalSlug]
			goalStatus.LastSnapshot = &snapshot
		}

		status.Goals[goalSlug] = goalStatus
	}

	return status
}

func (e *reminderEngine) stateForGoal(goalSlug string) *reminderState {
	state, ok := e.states[goalSlug]
	if ok {
		return state
	}

	state = &reminderState{}
	e.states[goalSlug] = state
	return state
}

type reminderStatus struct {
	Commands controlCommands               `json:"commands"`
	Goals    map[string]goalReminderStatus `json:"goals"`
}

type goalReminderStatus struct {
	CurrentDaystamp     string         `json:"currentDaystamp"`
	Completed           bool           `json:"completed"`
	NextReminderAt      *time.Time     `json:"nextReminderAt,omitempty"`
	SnoozedUntil        *time.Time     `json:"snoozedUntil,omitempty"`
	ActiveUntil         *time.Time     `json:"activeUntil,omitempty"`
	LastReminderAt      *time.Time     `json:"lastReminderAt,omitempty"`
	LastReminderMessage string         `json:"lastReminderMessage,omitempty"`
	LastSnapshot        *dailySnapshot `json:"lastSnapshot,omitempty"`
}

func reminderIntervalForLocalTime(localNow time.Time, cfg config) time.Duration {
	if len(cfg.ReminderSchedule) == 0 {
		return cfg.ReminderInterval
	}

	minuteOfDay := localNow.Hour()*60 + localNow.Minute()
	interval := cfg.ReminderInterval
	for _, step := range cfg.ReminderSchedule {
		if minuteOfDay < step.StartMinuteOfDay {
			break
		}
		interval = step.Interval
	}

	return interval
}

func isAtOrAfterBedtime(localNow time.Time, bedtimeHour, bedtimeMinute int) bool {
	if localNow.Hour() > bedtimeHour {
		return true
	}

	if localNow.Hour() == bedtimeHour && localNow.Minute() >= bedtimeMinute {
		return true
	}

	return false
}

func optionalTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}

	copy := value
	return &copy
}
