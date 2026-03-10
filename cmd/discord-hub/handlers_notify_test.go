package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"

	"personal-infrastructure/pkg/hubnotify"
)

type fakeMessageSender struct {
	calls      int
	channelID  string
	message    *discordgo.MessageSend
	sendErr    error
	lastOption int
	waitForCtx bool
}

func (f *fakeMessageSender) ChannelMessageSendComplex(channelID string, message *discordgo.MessageSend, options ...discordgo.RequestOption) (*discordgo.Message, error) {
	f.calls++
	f.channelID = channelID
	f.message = message
	f.lastOption = len(options)
	if f.waitForCtx {
		cfg := &discordgo.RequestConfig{Request: httptest.NewRequest(http.MethodPost, "https://discord.invalid/api", nil)}
		for _, option := range options {
			option(cfg)
		}
		<-cfg.Request.Context().Done()
		return nil, cfg.Request.Context().Err()
	}

	if f.sendErr != nil {
		return nil, f.sendErr
	}

	return &discordgo.Message{ID: "msg-1"}, nil
}

func testHubHandler(sender discordMessageSender) *hubHandler {
	return &hubHandler{
		log:             log.New(io.Discard, "", 0),
		session:         sender,
		channelNameToID: map[string]string{"alerts": "123"},
		actionCallbacks: newActionCallbackRegistry(time.Hour),
		notifyAuthToken: "test-notify-token",
	}
}

func authorizeNotifyRequest(req *http.Request) {
	req.Header.Set("Authorization", "Bearer test-notify-token")
}

func TestValidateNotifyPayloadTrimsAndNormalizesSeverity(t *testing.T) {
	payload := &notifyPayload{
		Version:               hubnotify.Version2,
		TargetChannel:         " alerts ",
		Service:               " beeminder-spoke ",
		Event:                 " trigger-fired ",
		Severity:              " CRITICAL ",
		Title:                 "[BEEMINDER-SPOKE] TRIGGER FIRED",
		Summary:               " hello world ",
		Fields:                []hubnotify.NotifyField{{Key: " Goal ", Value: " Study ", Group: " Context ", Order: 10, Inline: false}},
		Actions:               []hubnotify.NotifyAction{},
		AllowedMentions:       hubnotify.AllowedMentions{Parse: []string{" USERS "}, Users: []string{}, Roles: []string{}, RepliedUser: false},
		Visibility:            " PUBLIC ",
		SuppressNotifications: true,
		OccurredAt:            time.Date(2026, time.March, 10, 14, 22, 0, 0, time.UTC),
	}

	if err := validateNotifyPayload(payload); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if payload.TargetChannel != "alerts" {
		t.Fatalf("unexpected target channel: %q", payload.TargetChannel)
	}
	if payload.Service != "beeminder-spoke" || payload.Event != "trigger-fired" {
		t.Fatalf("unexpected service/event: %#v", payload)
	}
	if payload.Severity != "critical" {
		t.Fatalf("unexpected severity: %q", payload.Severity)
	}
	if payload.Summary != "hello world" {
		t.Fatalf("unexpected summary: %q", payload.Summary)
	}
	if payload.Visibility != hubnotify.VisibilityPublic {
		t.Fatalf("unexpected visibility: %q", payload.Visibility)
	}
	if payload.Fields[0].Key != "Goal" || payload.Fields[0].Value != "Study" || payload.Fields[0].Group != hubnotify.FieldGroupContext {
		t.Fatalf("unexpected field normalization: %#v", payload.Fields[0])
	}
	if !reflect.DeepEqual(payload.AllowedMentions.Parse, []string{"users"}) {
		t.Fatalf("unexpected allowed mentions parse: %#v", payload.AllowedMentions.Parse)
	}
}

func TestValidateNotifyPayloadRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name    string
		payload notifyPayload
		wantErr string
	}{
		{
			name: "missing version",
			payload: notifyPayload{
				TargetChannel:         "alerts",
				Service:               "beeminder-spoke",
				Event:                 "trigger-fired",
				Severity:              "info",
				Title:                 "[BEEMINDER-SPOKE] TRIGGER FIRED",
				Summary:               "ok",
				Fields:                []hubnotify.NotifyField{{Key: "Goal", Value: "Study", Group: "Context", Order: 10, Inline: false}},
				Actions:               []hubnotify.NotifyAction{},
				AllowedMentions:       hubnotify.AllowedMentions{Parse: []string{}, Users: []string{}, Roles: []string{}, RepliedUser: false},
				Visibility:            "public",
				SuppressNotifications: false,
				OccurredAt:            time.Now().UTC(),
			},
			wantErr: "version must be 2",
		},
		{
			name: "non-public visibility",
			payload: notifyPayload{
				Version:               hubnotify.Version2,
				TargetChannel:         "alerts",
				Service:               "beeminder-spoke",
				Event:                 "trigger-fired",
				Severity:              "warning",
				Title:                 "[BEEMINDER-SPOKE] TRIGGER FIRED",
				Summary:               "ok",
				Fields:                []hubnotify.NotifyField{{Key: "Goal", Value: "Study", Group: "Context", Order: 10, Inline: false}},
				Actions:               []hubnotify.NotifyAction{},
				AllowedMentions:       hubnotify.AllowedMentions{Parse: []string{}, Users: []string{}, Roles: []string{}, RepliedUser: false},
				Visibility:            "ephemeral",
				SuppressNotifications: false,
				OccurredAt:            time.Now().UTC(),
			},
			wantErr: "visibility must be public",
		},
		{
			name: "missing target channel",
			payload: notifyPayload{
				Version:               hubnotify.Version2,
				Service:               "beeminder-spoke",
				Event:                 "trigger-fired",
				Severity:              "info",
				Title:                 "[BEEMINDER-SPOKE] TRIGGER FIRED",
				Summary:               "ok",
				Fields:                []hubnotify.NotifyField{{Key: "Goal", Value: "Study", Group: "Context", Order: 10, Inline: false}},
				Actions:               []hubnotify.NotifyAction{},
				AllowedMentions:       hubnotify.AllowedMentions{Parse: []string{}, Users: []string{}, Roles: []string{}, RepliedUser: false},
				Visibility:            "public",
				SuppressNotifications: false,
				OccurredAt:            time.Now().UTC(),
			},
			wantErr: "targetChannel is required",
		},
		{
			name: "missing summary",
			payload: notifyPayload{
				Version:               hubnotify.Version2,
				TargetChannel:         "alerts",
				Service:               "beeminder-spoke",
				Event:                 "trigger-fired",
				Severity:              "warning",
				Title:                 "[BEEMINDER-SPOKE] TRIGGER FIRED",
				Fields:                []hubnotify.NotifyField{{Key: "Goal", Value: "Study", Group: "Context", Order: 10, Inline: false}},
				Actions:               []hubnotify.NotifyAction{},
				AllowedMentions:       hubnotify.AllowedMentions{Parse: []string{}, Users: []string{}, Roles: []string{}, RepliedUser: false},
				Visibility:            "public",
				SuppressNotifications: false,
				OccurredAt:            time.Now().UTC(),
			},
			wantErr: "summary is required",
		},
		{
			name: "invalid severity",
			payload: notifyPayload{
				Version:               hubnotify.Version2,
				TargetChannel:         "alerts",
				Service:               "beeminder-spoke",
				Event:                 "trigger-fired",
				Severity:              "fatal",
				Title:                 "[BEEMINDER-SPOKE] TRIGGER FIRED",
				Summary:               "ok",
				Fields:                []hubnotify.NotifyField{{Key: "Goal", Value: "Study", Group: "Context", Order: 10, Inline: false}},
				Actions:               []hubnotify.NotifyAction{},
				AllowedMentions:       hubnotify.AllowedMentions{Parse: []string{}, Users: []string{}, Roles: []string{}, RepliedUser: false},
				Visibility:            "public",
				SuppressNotifications: false,
				OccurredAt:            time.Now().UTC(),
			},
			wantErr: "severity must be one of: info, warning, critical",
		},
		{
			name: "title must match canonical format",
			payload: notifyPayload{
				Version:               hubnotify.Version2,
				TargetChannel:         "alerts",
				Service:               "beeminder-spoke",
				Event:                 "trigger-fired",
				Severity:              "warning",
				Title:                 "[BEEMINDER] TRIGGER FIRED",
				Summary:               "ok",
				Fields:                []hubnotify.NotifyField{{Key: "Goal", Value: "Study", Group: "Context", Order: 10, Inline: false}},
				Actions:               []hubnotify.NotifyAction{},
				AllowedMentions:       hubnotify.AllowedMentions{Parse: []string{}, Users: []string{}, Roles: []string{}, RepliedUser: false},
				Visibility:            "public",
				SuppressNotifications: false,
				OccurredAt:            time.Now().UTC(),
			},
			wantErr: "title must match the canonical [SERVICE] EVENT format for service/event",
		},
		{
			name: "conflicting allowed mentions users parse",
			payload: notifyPayload{
				Version:       hubnotify.Version2,
				TargetChannel: "alerts",
				Service:       "beeminder-spoke",
				Event:         "trigger-fired",
				Severity:      "warning",
				Title:         "[BEEMINDER-SPOKE] TRIGGER FIRED",
				Summary:       "ok",
				Fields:        []hubnotify.NotifyField{{Key: "Goal", Value: "Study", Group: "Context", Order: 10, Inline: false}},
				Actions:       []hubnotify.NotifyAction{},
				AllowedMentions: hubnotify.AllowedMentions{
					Parse:       []string{"users"},
					Users:       []string{"12345678901234567"},
					Roles:       []string{},
					RepliedUser: false,
				},
				Visibility:            "public",
				SuppressNotifications: false,
				OccurredAt:            time.Now().UTC(),
			},
			wantErr: "allowedMentions.users cannot be used when parse includes users",
		},
		{
			name: "duplicate allowed mentions user ids",
			payload: notifyPayload{
				Version:       hubnotify.Version2,
				TargetChannel: "alerts",
				Service:       "beeminder-spoke",
				Event:         "trigger-fired",
				Severity:      "warning",
				Title:         "[BEEMINDER-SPOKE] TRIGGER FIRED",
				Summary:       "ok",
				Fields:        []hubnotify.NotifyField{{Key: "Goal", Value: "Study", Group: "Context", Order: 10, Inline: false}},
				Actions:       []hubnotify.NotifyAction{},
				AllowedMentions: hubnotify.AllowedMentions{
					Parse:       []string{},
					Users:       []string{"12345678901234567", "12345678901234567"},
					Roles:       []string{},
					RepliedUser: false,
				},
				Visibility:            "public",
				SuppressNotifications: false,
				OccurredAt:            time.Now().UTC(),
			},
			wantErr: "allowedMentions.users must not contain duplicates",
		},
		{
			name: "invalid allowed mentions role id",
			payload: notifyPayload{
				Version:       hubnotify.Version2,
				TargetChannel: "alerts",
				Service:       "beeminder-spoke",
				Event:         "trigger-fired",
				Severity:      "warning",
				Title:         "[BEEMINDER-SPOKE] TRIGGER FIRED",
				Summary:       "ok",
				Fields:        []hubnotify.NotifyField{{Key: "Goal", Value: "Study", Group: "Context", Order: 10, Inline: false}},
				Actions:       []hubnotify.NotifyAction{},
				AllowedMentions: hubnotify.AllowedMentions{
					Parse:       []string{},
					Users:       []string{},
					Roles:       []string{"not-a-snowflake"},
					RepliedUser: false,
				},
				Visibility:            "public",
				SuppressNotifications: false,
				OccurredAt:            time.Now().UTC(),
			},
			wantErr: "allowedMentions.roles must only include Discord snowflake IDs",
		},
		{
			name: "non-link action requires callback url",
			payload: notifyPayload{
				Version:       hubnotify.Version2,
				TargetChannel: "alerts",
				Service:       "beeminder-spoke",
				Event:         "trigger-fired",
				Severity:      "warning",
				Title:         "[BEEMINDER-SPOKE] TRIGGER FIRED",
				Summary:       "ok",
				Fields:        []hubnotify.NotifyField{{Key: "Goal", Value: "Study", Group: "Context", Order: 10, Inline: false}},
				Actions:       []hubnotify.NotifyAction{{ID: "snooze_10m", Label: "Snooze 10m", Style: hubnotify.ActionStyleSecondary}},
				AllowedMentions: hubnotify.AllowedMentions{
					Parse:       []string{},
					Users:       []string{},
					Roles:       []string{},
					RepliedUser: false,
				},
				Visibility:            "public",
				SuppressNotifications: false,
				OccurredAt:            time.Now().UTC(),
			},
			wantErr: "callbackUrl is required when actions include non-link buttons",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := tt.payload
			err := validateNotifyPayload(&payload)
			if err == nil {
				t.Fatalf("expected error %q, got nil", tt.wantErr)
			}
			if err.Error() != tt.wantErr {
				t.Fatalf("expected error %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestNotifySendsDiscordMessageAndReturns202(t *testing.T) {
	sender := &fakeMessageSender{}
	h := testHubHandler(sender)

	req := httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(`{"version":2,"targetChannel":"alerts","service":"beeminder-spoke","event":"trigger-fired","severity":"critical","title":"[BEEMINDER-SPOKE] TRIGGER FIRED","summary":"Disk is full","fields":[{"key":"Goal","value":"Study","group":"Context","order":10,"inline":false}],"actions":[],"allowedMentions":{"parse":[],"users":[],"roles":[],"repliedUser":false},"visibility":"public","suppressNotifications":true,"occurredAt":"2026-03-10T14:22:00Z"}`))
	authorizeNotifyRequest(req)
	rec := httptest.NewRecorder()

	h.notify(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, rec.Code)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != `{"status":"sent"}` {
		t.Fatalf("unexpected response body: %q", body)
	}
	if sender.calls != 1 {
		t.Fatalf("expected exactly one send call, got %d", sender.calls)
	}
	if sender.channelID != "123" {
		t.Fatalf("unexpected channel ID: %q", sender.channelID)
	}
	if sender.message == nil || len(sender.message.Embeds) != 1 {
		t.Fatalf("expected one embed payload, got %#v", sender.message)
	}
	if sender.message.Embeds[0].Title != "[BEEMINDER-SPOKE] TRIGGER FIRED" {
		t.Fatalf("unexpected embed title: %#v", sender.message.Embeds[0])
	}
	if sender.message.Embeds[0].Color != 0xCF222E {
		t.Fatalf("unexpected embed color: %d", sender.message.Embeds[0].Color)
	}
	if sender.message.AllowedMentions == nil || len(sender.message.AllowedMentions.Parse) != 0 {
		t.Fatalf("unexpected allowed mentions: %#v", sender.message.AllowedMentions)
	}
	if sender.message.Flags != discordgo.MessageFlagsSuppressNotifications {
		t.Fatalf("unexpected message flags: %v", sender.message.Flags)
	}
	if sender.lastOption == 0 {
		t.Fatal("expected notify send to include request options")
	}
}

func TestBuildNotifyMessageIncludesExplicitAllowedMentions(t *testing.T) {
	payload := notifyPayload{
		Version:       hubnotify.Version2,
		TargetChannel: "alerts",
		Service:       "accountability-spoke",
		Event:         "commitment-reminder",
		Severity:      hubnotify.SeverityWarning,
		Title:         hubnotify.CanonicalTitle("accountability-spoke", "commitment-reminder"),
		Summary:       "Reminder for <@12345678901234567>",
		Fields:        []hubnotify.NotifyField{{Key: "Task", Value: "Write tests", Group: hubnotify.FieldGroupContext, Order: 10, Inline: false}},
		Actions:       []hubnotify.NotifyAction{},
		AllowedMentions: hubnotify.AllowedMentions{
			Parse:       []string{},
			Users:       []string{"12345678901234567"},
			Roles:       []string{},
			RepliedUser: false,
		},
		Visibility:            hubnotify.VisibilityPublic,
		SuppressNotifications: false,
		OccurredAt:            time.Date(2026, time.March, 10, 15, 0, 0, 0, time.UTC),
	}

	message, err := buildNotifyMessage(payload, newActionCallbackRegistry(time.Hour))
	if err != nil {
		t.Fatalf("buildNotifyMessage returned error: %v", err)
	}
	if message.AllowedMentions == nil {
		t.Fatal("expected allowed mentions on built message")
	}
	if !reflect.DeepEqual(message.AllowedMentions.Users, []string{"12345678901234567"}) {
		t.Fatalf("unexpected users allowlist: %#v", message.AllowedMentions.Users)
	}
	if message.Embeds[0].Color != 0xD29922 {
		t.Fatalf("unexpected warning color: %d", message.Embeds[0].Color)
	}
	if message.Embeds[0].Footer == nil || message.Embeds[0].Footer.Text != encodeNotifyFooter("accountability-spoke", "commitment-reminder") {
		t.Fatalf("unexpected embed footer metadata: %#v", message.Embeds[0].Footer)
	}
}

func TestBuildNotifyMessageIncludesButtons(t *testing.T) {
	payload := notifyPayload{
		Version:         hubnotify.Version2,
		TargetChannel:   "alerts",
		CallbackURL:     "http://127.0.0.1:8092/callback",
		Service:         "beeminder-spoke",
		Event:           "goal-reminder",
		Severity:        hubnotify.SeverityWarning,
		Title:           hubnotify.CanonicalTitle("beeminder-spoke", "goal-reminder"),
		Summary:         "Time to make progress.",
		Fields:          []hubnotify.NotifyField{{Key: "Goal", Value: "study", Group: hubnotify.FieldGroupContext, Order: 10, Inline: false}},
		Actions:         []hubnotify.NotifyAction{{ID: "snooze_10m:study", Label: "Snooze 10m", Style: hubnotify.ActionStyleSecondary}},
		AllowedMentions: hubnotify.AllowedMentions{Parse: []string{}, Users: []string{}, Roles: []string{}, RepliedUser: false},
		Visibility:      hubnotify.VisibilityPublic,
		OccurredAt:      time.Date(2026, time.March, 10, 15, 0, 0, 0, time.UTC),
	}

	registry := newActionCallbackRegistry(time.Hour)
	message, err := buildNotifyMessage(payload, registry)
	if err != nil {
		t.Fatalf("buildNotifyMessage returned error: %v", err)
	}
	if len(message.Components) != 1 {
		t.Fatalf("expected one action row, got %#v", message.Components)
	}
	row, ok := message.Components[0].(discordgo.ActionsRow)
	if !ok || len(row.Components) != 1 {
		t.Fatalf("unexpected action row: %#v", message.Components[0])
	}
	button, ok := row.Components[0].(discordgo.Button)
	if !ok {
		t.Fatalf("unexpected component type: %#v", row.Components[0])
	}
	if button.CustomID != encodeNotifyActionCustomID(hubnotify.CallbackToken("http://127.0.0.1:8092/callback"), "snooze_10m:study") {
		t.Fatalf("unexpected custom ID: %q", button.CustomID)
	}
	if callbackURL, ok := registry.Resolve(hubnotify.CallbackToken("http://127.0.0.1:8092/callback")); !ok || callbackURL != "http://127.0.0.1:8092/callback" {
		t.Fatalf("callback token was not registered: %q %v", callbackURL, ok)
	}
}

func TestNotifyUnknownChannelReturns400(t *testing.T) {
	sender := &fakeMessageSender{}
	h := testHubHandler(sender)

	req := httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(`{"version":2,"targetChannel":"missing","service":"beeminder-spoke","event":"trigger-fired","severity":"info","title":"[BEEMINDER-SPOKE] TRIGGER FIRED","summary":"hello","fields":[{"key":"Goal","value":"Study","group":"Context","order":10,"inline":false}],"actions":[],"allowedMentions":{"parse":[],"users":[],"roles":[],"repliedUser":false},"visibility":"public","suppressNotifications":false,"occurredAt":"2026-03-10T14:22:00Z"}`))
	authorizeNotifyRequest(req)
	rec := httptest.NewRecorder()

	h.notify(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
	if sender.calls != 0 {
		t.Fatalf("expected no send call, got %d", sender.calls)
	}
}

func TestNotifyDiscordFailureReturns502(t *testing.T) {
	sender := &fakeMessageSender{sendErr: io.ErrUnexpectedEOF}
	h := testHubHandler(sender)

	req := httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(`{"version":2,"targetChannel":"alerts","service":"beeminder-spoke","event":"trigger-fired","severity":"warning","title":"[BEEMINDER-SPOKE] TRIGGER FIRED","summary":"hello","fields":[{"key":"Goal","value":"Study","group":"Context","order":10,"inline":false}],"actions":[],"allowedMentions":{"parse":[],"users":[],"roles":[],"repliedUser":false},"visibility":"public","suppressNotifications":false,"occurredAt":"2026-03-10T14:22:00Z"}`))
	authorizeNotifyRequest(req)
	rec := httptest.NewRecorder()

	h.notify(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected status %d, got %d", http.StatusBadGateway, rec.Code)
	}
}

func TestNotifyDiscordTimeoutReturns504(t *testing.T) {
	sender := &fakeMessageSender{waitForCtx: true}
	h := testHubHandler(sender)

	req := httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(`{"version":2,"targetChannel":"alerts","service":"beeminder-spoke","event":"trigger-fired","severity":"warning","title":"[BEEMINDER-SPOKE] TRIGGER FIRED","summary":"hello","fields":[{"key":"Goal","value":"Study","group":"Context","order":10,"inline":false}],"actions":[],"allowedMentions":{"parse":[],"users":[],"roles":[],"repliedUser":false},"visibility":"public","suppressNotifications":false,"occurredAt":"2026-03-10T14:22:00Z"}`))
	ctx, cancel := context.WithCancel(req.Context())
	cancel()
	req = req.WithContext(ctx)
	authorizeNotifyRequest(req)
	rec := httptest.NewRecorder()

	h.notify(rec, req)

	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected status %d, got %d", http.StatusGatewayTimeout, rec.Code)
	}
}

func TestNotifyRejectsNonPostRequests(t *testing.T) {
	h := testHubHandler(&fakeMessageSender{})

	req := httptest.NewRequest(http.MethodGet, "/notify", nil)
	rec := httptest.NewRecorder()

	h.notify(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestNotifyMissingBearerTokenReturns401(t *testing.T) {
	h := testHubHandler(&fakeMessageSender{})

	req := httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(`{"version":2,"targetChannel":"alerts","service":"beeminder-spoke","event":"trigger-fired","severity":"warning","title":"[BEEMINDER-SPOKE] TRIGGER FIRED","summary":"hello","fields":[{"key":"Goal","value":"Study","group":"Context","order":10,"inline":false}],"actions":[],"allowedMentions":{"parse":[],"users":[],"roles":[],"repliedUser":false},"visibility":"public","suppressNotifications":false,"occurredAt":"2026-03-10T14:22:00Z"}`))
	rec := httptest.NewRecorder()

	h.notify(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestNotifyInvalidBearerTokenReturns401(t *testing.T) {
	h := testHubHandler(&fakeMessageSender{})

	req := httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(`{"version":2,"targetChannel":"alerts","service":"beeminder-spoke","event":"trigger-fired","severity":"warning","title":"[BEEMINDER-SPOKE] TRIGGER FIRED","summary":"hello","fields":[{"key":"Goal","value":"Study","group":"Context","order":10,"inline":false}],"actions":[],"allowedMentions":{"parse":[],"users":[],"roles":[],"repliedUser":false},"visibility":"public","suppressNotifications":false,"occurredAt":"2026-03-10T14:22:00Z"}`))
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()

	h.notify(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}
