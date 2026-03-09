package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
