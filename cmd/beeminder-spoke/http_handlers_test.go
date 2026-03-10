package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"personal-infrastructure/pkg/hubnotify"
)

func TestHandleCommandRejectsMissingDiscordUserID(t *testing.T) {
	app := newTestApp(testConfig())
	req := httptest.NewRequest(http.MethodPost, "/control/command", strings.NewReader(`{"command":"status","context":{}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	app.handleCommand(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "context.discordUserId is required") {
		t.Fatalf("unexpected response body: %q", rec.Body.String())
	}
}

func TestHandleSnoozeSupportsLegacyMinutes(t *testing.T) {
	app := newTestApp(testConfig())
	req := httptest.NewRequest(http.MethodPost, "/control/snooze", strings.NewReader(`{"minutes":15}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	app.handleSnooze(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"duration":"15m0s"`) {
		t.Fatalf("expected converted duration in response, got %q", body)
	}
}

func TestHandleCommandsReturnsCatalog(t *testing.T) {
	app := newTestApp(testConfig())
	req := httptest.NewRequest(http.MethodGet, "/control/commands", nil)
	rec := httptest.NewRecorder()

	app.handleCommands(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"service":"beeminder-spoke"`) {
		t.Fatalf("missing service name in response: %q", body)
	}
	if !strings.Contains(body, `"name":"b-snooze"`) {
		t.Fatalf("missing snooze command in response: %q", body)
	}
}

func TestHandleDiscordCallbackSnoozesViaExistingCommand(t *testing.T) {
	app := newTestApp(testConfig())
	reqBody := hubnotify.ActionCallbackRequest{
		Version:  hubnotify.Version2,
		Service:  commandCatalogService,
		Event:    beeminderNotifyEvent,
		Severity: hubnotify.SeverityWarning,
		Action: hubnotify.ActionCallbackAction{
			ID:    discordActionSnooze10m,
			Label: "Snooze 10m",
			Style: hubnotify.ActionStyleSecondary,
		},
		Context: hubnotify.ActionCallbackContext{DiscordUserID: "u-123"},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("marshal callback request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/discord/callback", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-callback-token")
	rec := httptest.NewRecorder()

	app.handleDiscordCallback(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"status":"ok"`) || !strings.Contains(rec.Body.String(), `Snoozed reminders for 10m0s`) {
		t.Fatalf("unexpected callback response: %q", rec.Body.String())
	}
	status := app.engine.Status()
	goalStatus, ok := status.Goals["study"]
	if !ok {
		t.Fatalf("expected study goal status, got %#v", status.Goals)
	}
	if goalStatus.SnoozedUntil == nil {
		t.Fatal("expected engine snoozed until to be set")
	}
	if remaining := time.Until(*goalStatus.SnoozedUntil); remaining <= 9*time.Minute || remaining > 11*time.Minute {
		t.Fatalf("unexpected snooze duration remaining: %v", remaining)
	}
}

func TestHandleDiscordCallbackRejectsUnknownAction(t *testing.T) {
	app := newTestApp(testConfig())
	req := httptest.NewRequest(http.MethodPost, "/discord/callback", strings.NewReader(`{"version":2,"service":"beeminder-spoke","event":"goal-reminder","action":{"id":"nope","label":"Nope","style":"secondary"},"context":{"discordUserId":"u-123"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-callback-token")
	rec := httptest.NewRecorder()

	app.handleDiscordCallback(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "unknown action id") {
		t.Fatalf("unexpected response body: %q", rec.Body.String())
	}
}

func TestHandleDiscordCallbackRejectsUnauthorizedRequest(t *testing.T) {
	app := newTestApp(testConfig())
	req := httptest.NewRequest(http.MethodPost, "/discord/callback", strings.NewReader(`{"version":2,"service":"beeminder-spoke","event":"goal-reminder","action":{"id":"snooze_10m","label":"Snooze 10m","style":"secondary"},"context":{"discordUserId":"u-123"}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	app.handleDiscordCallback(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
