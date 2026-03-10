package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"personal-infrastructure/pkg/accountability"
	"personal-infrastructure/pkg/hubnotify"
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

	attachment = parseAttachment("definitely not an attachment")
	if attachment != (accountability.AttachmentMetadata{}) {
		t.Fatalf("expected scalar attachment payload to be ignored, got %#v", attachment)
	}
}

func TestHandleCommandRejectsWrongMethod(t *testing.T) {
	app := newTestSpokeApp(t)
	req := httptest.NewRequest(http.MethodGet, "/control/command", nil)
	rec := httptest.NewRecorder()

	app.handleCommand(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
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

func TestHandleCommandProofRejectsScalarAttachmentPayload(t *testing.T) {
	app := newTestSpokeApp(t)
	_, err := app.service.CommitWithPolicy(context.Background(), "u1", "gym", time.Now().UTC().Add(time.Hour), "default", policyEngineAttachment, `{}`)
	if err != nil {
		t.Fatal(err)
	}

	rec := performCommandRequest(t, app, `{"command":"proof","context":{"discordUserId":"u1"},"options":{"proof":"sure, trust me"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "attachment proof") {
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
				DiscordCallbackURL: "http://127.0.0.1:8093/discord/callback",
				CallbackAuthToken:  "test-callback-token",
				NotifyChannel:      "accountability",
				NotifySeverity:     "warning",
				ReminderInterval:   5 * time.Minute,
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

func TestHandleDiscordCallbackSnooze30m(t *testing.T) {
	app := newTestSpokeApp(t)
	_, err := app.service.Commit(context.Background(), "u1", "ship feature", time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/discord/callback", strings.NewReader(`{"version":2,"service":"accountability-spoke","event":"commitment-reminder","action":{"id":"snooze_30m","label":"Snooze 30m","style":"secondary"},"context":{"discordUserId":"u1"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-callback-token")
	rec := httptest.NewRecorder()

	app.handleDiscordCallback(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Reminders snoozed until") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestHandleDiscordCallbackDismissUsesDefaultSnooze(t *testing.T) {
	app := newTestSpokeApp(t)
	_, err := app.service.Commit(context.Background(), "u1", "ship feature", time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/discord/callback", strings.NewReader(`{"version":2,"service":"accountability-spoke","event":"commitment-reminder","action":{"id":"dismiss","label":"Dismiss","style":"secondary"},"context":{"discordUserId":"u1"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-callback-token")
	rec := httptest.NewRecorder()

	app.handleDiscordCallback(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Dismissed this reminder until") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestHandleDiscordCallbackRejectsUnauthorizedRequest(t *testing.T) {
	app := newTestSpokeApp(t)
	req := httptest.NewRequest(http.MethodPost, "/discord/callback", strings.NewReader(`{"version":2,"service":"accountability-spoke","event":"commitment-reminder","action":{"id":"dismiss","label":"Dismiss","style":"secondary"},"context":{"discordUserId":"u1"}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	app.handleDiscordCallback(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestRunReminderSweepIncludesCallbackActions(t *testing.T) {
	app := newTestSpokeApp(t)
	commitment, err := app.service.Commit(context.Background(), "u1", "ship feature", time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.serviceDB.Exec(`UPDATE commitments SET deadline=? WHERE id=?`, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339), commitment.ID); err != nil {
		t.Fatalf("update overdue deadline: %v", err)
	}

	var notifyPayload hubnotify.NotifyRequest
	hubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&notifyPayload); err != nil {
			t.Fatalf("decode notify payload: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer hubServer.Close()

	hub := hubnotify.NewClient(hubServer.URL, "hub-token", hubServer.Client())
	if err := runReminderSweep(context.Background(), log.New(io.Discard, "", 0), app.cfg, app.service, hub); err != nil {
		t.Fatalf("runReminderSweep returned error: %v", err)
	}

	if notifyPayload.CallbackURL != app.cfg.DiscordCallbackURL {
		t.Fatalf("callback url = %q, want %q", notifyPayload.CallbackURL, app.cfg.DiscordCallbackURL)
	}
	if len(notifyPayload.Actions) != 2 {
		t.Fatalf("action count = %d, want 2", len(notifyPayload.Actions))
	}
	if notifyPayload.Actions[0].ID != accountabilityActionSnooze30m || notifyPayload.Actions[1].ID != accountabilityActionDismiss {
		t.Fatalf("unexpected actions: %#v", notifyPayload.Actions)
	}
}
