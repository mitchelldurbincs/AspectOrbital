package main

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestStateStoreMarkRunAndSummarySent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "finance-state.json")
	store, err := newStateStore(path)
	if err != nil {
		t.Fatalf("newStateStore() error = %v", err)
	}

	runAt := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	runErr := errors.New("plaid timeout")
	if err := store.MarkRun("2026-03-01", runAt, runErr); err != nil {
		t.Fatalf("MarkRun() error = %v", err)
	}

	snapshot := store.Snapshot()
	if snapshot.LastRunWeekKey != "2026-03-01" || snapshot.LastRunSucceeded || snapshot.LastRunError == "" {
		t.Fatalf("unexpected run snapshot: %+v", snapshot)
	}

	sentAt := runAt.Add(time.Minute)
	if err := store.MarkSummarySent("2026-03-01", sentAt, 3, 42.15); err != nil {
		t.Fatalf("MarkSummarySent() error = %v", err)
	}

	reloaded, err := newStateStore(path)
	if err != nil {
		t.Fatalf("newStateStore(reloaded) error = %v", err)
	}
	got := reloaded.Snapshot()
	if got.LastSentWeekKey != "2026-03-01" || got.LastSentCount != 3 || got.LastSentTotal != 42.15 {
		t.Fatalf("unexpected persisted snapshot: %+v", got)
	}
}
