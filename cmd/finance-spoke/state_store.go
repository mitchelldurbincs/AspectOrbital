package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type stateStore struct {
	mu    sync.Mutex
	path  string
	state financeState
}

type financeState struct {
	LastSentWeekKey string    `json:"lastSentWeekKey"`
	LastSentAt      time.Time `json:"lastSentAt,omitempty"`
	LastSentCount   int       `json:"lastSentCount"`
	LastSentTotal   float64   `json:"lastSentTotal"`

	LastRunAt        time.Time `json:"lastRunAt,omitempty"`
	LastRunWeekKey   string    `json:"lastRunWeekKey"`
	LastRunError     string    `json:"lastRunError,omitempty"`
	LastRunSucceeded bool      `json:"lastRunSucceeded"`
}

func newStateStore(path string) (*stateStore, error) {
	store := &stateStore{path: path}
	if err := store.load(); err != nil {
		return nil, err
	}

	return store, nil
}

func (s *stateStore) Snapshot() financeState {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.state
}

func (s *stateStore) MarkRun(weekKey string, runAt time.Time, runErr error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state.LastRunWeekKey = weekKey
	s.state.LastRunAt = runAt
	s.state.LastRunSucceeded = runErr == nil
	if runErr != nil {
		s.state.LastRunError = runErr.Error()
	} else {
		s.state.LastRunError = ""
	}

	return s.saveLocked()
}

func (s *stateStore) MarkSummarySent(weekKey string, sentAt time.Time, count int, total float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state.LastSentWeekKey = weekKey
	s.state.LastSentAt = sentAt
	s.state.LastSentCount = count
	s.state.LastSentTotal = total

	return s.saveLocked()
}

func (s *stateStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	raw, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.state = financeState{}
			return nil
		}
		return err
	}

	if len(raw) == 0 {
		s.state = financeState{}
		return nil
	}

	if err := json.Unmarshal(raw, &s.state); err != nil {
		return err
	}

	return nil
}

func (s *stateStore) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	raw, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}

	tempPath := s.path + ".tmp"
	if err := os.WriteFile(tempPath, raw, 0o600); err != nil {
		return err
	}

	return os.Rename(tempPath, s.path)
}
