package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"personal-infrastructure/pkg/hubnotify"
)

func TestRunCycleStopsOnFirstGoalError(t *testing.T) {
	var secondGoalHit bool

	beeminderServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/goals/first.json"):
			http.Error(w, "boom", http.StatusInternalServerError)
		case strings.Contains(r.URL.Path, "/goals/second.json"):
			secondGoalHit = true
			fmt.Fprint(w, `{"slug":"second","rate":1,"runits":"d","deadline":0,"gunits":"units","aggday":"sum","delta":1}`)
		default:
			t.Fatalf("unexpected beeminder path: %s", r.URL.Path)
		}
	}))
	defer beeminderServer.Close()

	hubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer hubServer.Close()

	cfg := testConfig()
	cfg.BeeminderGoalSlugs = []string{"first", "second"}
	cfg.BeeminderBaseURL = beeminderServer.URL
	app := newTestAppWithClients(cfg, beeminderServer.Client(), hubServer.URL)

	err := app.runCycle(context.Background())
	if err == nil {
		t.Fatal("runCycle() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), `goal "first"`) {
		t.Fatalf("unexpected error: %v", err)
	}
	if secondGoalHit {
		t.Fatal("second goal was evaluated; expected fail-fast behavior")
	}
}

func TestRunGoalCycleSendsReminderWhenBehind(t *testing.T) {
	var notifyRequests []hubnotify.NotifyRequest

	beeminderServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/goals/study.json"):
			fmt.Fprint(w, `{"slug":"study","title":"Study","rate":1,"runits":"d","deadline":0,"gunits":"hours","aggday":"sum","delta":1}`)
		case strings.Contains(r.URL.Path, "/goals/study/datapoints.json"):
			fmt.Fprint(w, `[]`)
		default:
			t.Fatalf("unexpected beeminder path: %s", r.URL.Path)
		}
	}))
	defer beeminderServer.Close()

	hubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read notify body: %v", err)
		}
		var payload hubnotify.NotifyRequest
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("failed to decode notify payload: %v", err)
		}
		notifyRequests = append(notifyRequests, payload)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer hubServer.Close()

	cfg := testConfig()
	cfg.BeeminderBaseURL = beeminderServer.URL
	cfg.ActionURLs = map[string]string{"study": "https://example.com/start"}
	app := newTestAppWithClients(cfg, beeminderServer.Client(), hubServer.URL)

	now := time.Date(2026, time.March, 9, 17, 0, 0, 0, time.UTC)
	err := app.runGoalCycle(context.Background(), "study", now, now)
	if err != nil {
		t.Fatalf("runGoalCycle() error = %v", err)
	}
	if len(notifyRequests) != 1 {
		t.Fatalf("notify request count = %d, want 1", len(notifyRequests))
	}
	if notifyRequests[0].TargetChannel != cfg.NotifyTargetChannel {
		t.Fatalf("target channel = %q, want %q", notifyRequests[0].TargetChannel, cfg.NotifyTargetChannel)
	}
	if notifyRequests[0].Version != hubnotify.Version2 {
		t.Fatalf("version = %d, want %d", notifyRequests[0].Version, hubnotify.Version2)
	}
	if notifyRequests[0].Title != hubnotify.CanonicalTitle("beeminder-spoke", "goal-reminder") {
		t.Fatalf("unexpected title: %q", notifyRequests[0].Title)
	}
	if notifyRequests[0].CallbackURL != cfg.DiscordCallbackURL {
		t.Fatalf("callback url = %q, want %q", notifyRequests[0].CallbackURL, cfg.DiscordCallbackURL)
	}
	if !strings.Contains(notifyRequests[0].Summary, "study reminder") {
		t.Fatalf("unexpected reminder summary: %q", notifyRequests[0].Summary)
	}
	if len(notifyRequests[0].Actions) != 3 {
		t.Fatalf("action count = %d, want 3", len(notifyRequests[0].Actions))
	}
	if notifyRequests[0].Actions[0].ID != discordActionSnooze10m || notifyRequests[0].Actions[1].ID != discordActionSnooze30m || notifyRequests[0].Actions[2].ID != discordActionAcknowledge {
		t.Fatalf("unexpected actions: %#v", notifyRequests[0].Actions)
	}
	if len(notifyRequests[0].Fields) == 0 {
		t.Fatalf("expected rich notify fields, got %#v", notifyRequests[0])
	}
}
