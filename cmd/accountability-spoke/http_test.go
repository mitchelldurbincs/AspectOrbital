package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseDeadline(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		raw     string
		want    time.Time
		wantErr bool
	}{
		{name: "duration", raw: "2h", want: now.Add(2 * time.Hour)},
		{name: "unix", raw: "1735689600", want: time.Unix(1735689600, 0).UTC()},
		{name: "rfc3339", raw: "2026-01-02T03:04:05Z", want: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)},
		{name: "empty", raw: "", wantErr: true},
		{name: "invalid", raw: "banana", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseDeadline(now, tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.Equal(tc.want) {
				t.Fatalf("parseDeadline() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseAttachment(t *testing.T) {
	fromMap := parseAttachment(map[string]any{"id": " abc ", "filename": "proof.png", "url": "https://x", "content_type": "image/png"})
	if fromMap.ID != "abc" || fromMap.Filename != "proof.png" {
		t.Fatalf("unexpected map attachment: %+v", fromMap)
	}

	fromString := parseAttachment("file-id")
	if fromString.ID != "file-id" {
		t.Fatalf("unexpected string attachment: %+v", fromString)
	}
}

func TestHandleCommandValidation(t *testing.T) {
	app := &spokeApp{cfg: config{CommitCommandName: "commit", ProofCommandName: "proof", StatusCommandName: "status", CancelCommandName: "cancel"}}

	req := httptest.NewRequest(http.MethodPost, "/control/command", strings.NewReader(`{"command":"commit","options":{}}`))
	res := httptest.NewRecorder()
	app.handleCommand(res, req)
	if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), "discord_user_id is required") {
		t.Fatalf("expected discord_user_id validation error, got code=%d body=%q", res.Code, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/control/command", strings.NewReader(`{"command":"unknown","options":{"discord_user_id":"u1"}}`))
	res = httptest.NewRecorder()
	app.handleCommand(res, req)
	if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), "unknown command") {
		t.Fatalf("expected unknown command error, got code=%d body=%q", res.Code, res.Body.String())
	}
}
