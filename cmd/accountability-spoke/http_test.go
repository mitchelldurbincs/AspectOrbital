package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"personal-infrastructure/pkg/accountability"
)

func TestParseDeadlineClockTimeRollsToNextDay(t *testing.T) {
	now := time.Date(2026, time.March, 9, 20, 0, 0, 0, time.UTC)
	deadline, err := parseDeadline(now, "4:30am")
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, time.March, 10, 4, 30, 0, 0, time.UTC)
	if !deadline.Equal(want) {
		t.Fatalf("unexpected deadline\nwant: %s\ngot:  %s", want.Format(time.RFC3339), deadline.Format(time.RFC3339))
	}
}

func TestParseDeadlineClockTimeUsesSameDayWhenFuture(t *testing.T) {
	now := time.Date(2026, time.March, 9, 3, 0, 0, 0, time.UTC)
	deadline, err := parseDeadline(now, "4:30am")
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, time.March, 9, 4, 30, 0, 0, time.UTC)
	if !deadline.Equal(want) {
		t.Fatalf("unexpected deadline\nwant: %s\ngot:  %s", want.Format(time.RFC3339), deadline.Format(time.RFC3339))
	}
}

func TestParseAttachmentSupportsContentTypeAliasesAndSize(t *testing.T) {
	attachment := parseAttachment(map[string]any{
		"id":           " a1 ",
		"filename":     " proof.png ",
		"url":          " https://cdn.discordapp.com/proof.png ",
		"content_type": " image/png ",
		"size":         42.0,
	})
	if attachment.ID != "a1" {
		t.Fatalf("unexpected id: %q", attachment.ID)
	}
	if attachment.Filename != "proof.png" {
		t.Fatalf("unexpected filename: %q", attachment.Filename)
	}
	if attachment.URL != "https://cdn.discordapp.com/proof.png" {
		t.Fatalf("unexpected URL: %q", attachment.URL)
	}
	if attachment.ContentType != "image/png" {
		t.Fatalf("unexpected content type: %q", attachment.ContentType)
	}
	if attachment.Size != 42 {
		t.Fatalf("unexpected size: %d", attachment.Size)
	}

	attachment = parseAttachment(map[string]any{"contentType": "image/jpeg"})
	if attachment.ContentType != "image/jpeg" {
		t.Fatalf("expected camelCase contentType fallback, got: %q", attachment.ContentType)
	}
}

func TestHandleCommandCommitValidationErrorReturns400(t *testing.T) {
	app := newTestSpokeApp(t)
	rec := performCommandRequest(t, app, `{"command":"commit","context":{"discordUserId":"u1"},"options":{"deadline":"not-a-deadline"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "deadline must be RFC3339") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestHandleCommandCommitConflictReturns409(t *testing.T) {
	app := newTestSpokeApp(t)
	_, err := app.service.Commit(context.Background(), "u1", "existing task", time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	rec := performCommandRequest(t, app, `{"command":"commit","context":{"discordUserId":"u1"},"options":{"deadline":"1h"}}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "already have an active commitment") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestHandleCommandProofNotFoundReturns404(t *testing.T) {
	app := newTestSpokeApp(t)
	rec := performCommandRequest(t, app, `{"command":"proof","context":{"discordUserId":"u1"}}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "no active commitment") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestHandleCommandCheckInRequiresText(t *testing.T) {
	app := newTestSpokeApp(t)
	_, err := app.service.Commit(context.Background(), "u1", "gym", time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	rec := performCommandRequest(t, app, `{"command":"checkin","context":{"discordUserId":"u1"},"options":{"text":"   "}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "check-in text is required") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestHandleCommandCheckInReturnsQuietUntil(t *testing.T) {
	app := newTestSpokeApp(t)
	_, err := app.service.Commit(context.Background(), "u1", "gym", time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	rec := performCommandRequest(t, app, `{"command":"checkin","context":{"discordUserId":"u1"},"options":{"text":"getting ready"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Check-in recorded") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestHandleCommandCommitDatabaseFailureReturns500(t *testing.T) {
	app := newTestSpokeApp(t)
	if err := app.serviceDB.Close(); err != nil {
		t.Fatal(err)
	}

	rec := performCommandRequest(t, app, `{"command":"commit","context":{"discordUserId":"u1"},"options":{"deadline":"1h"}}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d (%s)", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) == "" {
		t.Fatal("expected error body for internal failure")
	}
}

type testSpokeApp struct {
	*spokeApp
	serviceDB *sql.DB
}

func newTestSpokeApp(t *testing.T) *testSpokeApp {
	t.Helper()

	path := filepath.Join(t.TempDir(), "accountability.sqlite")
	db, err := accountability.OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	if err := accountability.Bootstrap(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	service, err := accountability.NewService(db, time.Minute, 0)
	if err != nil {
		t.Fatal(err)
	}

	return &testSpokeApp{
		spokeApp: &spokeApp{
			cfg: config{
				CommitCommandName:  "commit",
				ProofCommandName:   "proof",
				CheckInCommandName: "checkin",
				StatusCommandName:  "status",
				CancelCommandName:  "cancel",
				SnoozeCommandName:  "snooze",
				CheckInQuietPeriod: 10 * time.Minute,
				DefaultSnooze:      10 * time.Minute,
				MaxSnooze:          time.Hour,
			},
			service: service,
			policies: policyCatalog{
				defaultPreset: "default",
				presets: map[string]resolvedPreset{
					"default": {Name: "default", Task: "default task", Engine: policyEngineAttachment, ConfigJSON: `{}`},
				},
			},
		},
		serviceDB: db,
	}
}

func performCommandRequest(t *testing.T, app *testSpokeApp, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/control/command", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.handleCommand(rec, req)
	return rec
}

func TestHandleCommandStatusWithoutCommitmentReturnsOK(t *testing.T) {
	app := newTestSpokeApp(t)
	rec := performCommandRequest(t, app, `{"command":"status","context":{"discordUserId":"u1"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["message"] != "No active commitment." {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}
