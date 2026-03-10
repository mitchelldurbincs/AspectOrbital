package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleCommandRejectsUnauthorizedRequest(t *testing.T) {
	app := &financeApp{cfg: config{SpokeCommandAuthToken: "test-command-token"}}
	req := httptest.NewRequest(http.MethodPost, "/control/command", strings.NewReader(`{"command":"finance-status","context":{"discordUserId":"u-1"}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	app.handleCommand(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestHandleCommandRejectsMissingDiscordUserID(t *testing.T) {
	app := &financeApp{cfg: config{SpokeCommandAuthToken: "test-command-token"}}
	req := httptest.NewRequest(http.MethodPost, "/control/command", strings.NewReader(`{"command":"finance-status","context":{}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-command-token")
	rec := httptest.NewRecorder()

	app.handleCommand(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "context.discordUserId is required") {
		t.Fatalf("unexpected response body: %q", rec.Body.String())
	}
}
