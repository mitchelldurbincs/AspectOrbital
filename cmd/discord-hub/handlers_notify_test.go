package main

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

type fakeMessageSender struct {
	calls      int
	channelID  string
	message    string
	sendErr    error
	lastOption int
}

func (f *fakeMessageSender) ChannelMessageSend(channelID string, content string, options ...discordgo.RequestOption) (*discordgo.Message, error) {
	f.calls++
	f.channelID = channelID
	f.message = content
	f.lastOption = len(options)

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
		criticalMention: "<@999>",
	}
}

func TestValidateNotifyPayloadTrimsAndNormalizesSeverity(t *testing.T) {
	payload := &notifyPayload{
		TargetChannel: " alerts ",
		Message:       " hello world ",
		Severity:      " CRITICAL ",
	}

	if err := validateNotifyPayload(payload); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if payload.TargetChannel != "alerts" {
		t.Fatalf("unexpected target channel: %q", payload.TargetChannel)
	}
	if payload.Message != "hello world" {
		t.Fatalf("unexpected message: %q", payload.Message)
	}
	if payload.Severity != "critical" {
		t.Fatalf("unexpected severity: %q", payload.Severity)
	}
}

func TestValidateNotifyPayloadRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name    string
		payload notifyPayload
		wantErr string
	}{
		{
			name: "missing target channel",
			payload: notifyPayload{
				Message:  "ok",
				Severity: "info",
			},
			wantErr: "targetChannel is required",
		},
		{
			name: "missing message",
			payload: notifyPayload{
				TargetChannel: "alerts",
				Severity:      "warning",
			},
			wantErr: "message is required",
		},
		{
			name: "invalid severity",
			payload: notifyPayload{
				TargetChannel: "alerts",
				Message:       "ok",
				Severity:      "fatal",
			},
			wantErr: "severity must be one of: info, warning, critical",
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

	req := httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(`{"targetChannel":"alerts","message":"Disk is full","severity":"critical"}`))
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
	if sender.message != "<@999> Disk is full" {
		t.Fatalf("unexpected message payload: %q", sender.message)
	}
}

func TestNotifyUnknownChannelReturns400(t *testing.T) {
	sender := &fakeMessageSender{}
	h := testHubHandler(sender)

	req := httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(`{"targetChannel":"missing","message":"hello","severity":"info"}`))
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

	req := httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(`{"targetChannel":"alerts","message":"hello","severity":"warning"}`))
	rec := httptest.NewRecorder()

	h.notify(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected status %d, got %d", http.StatusBadGateway, rec.Code)
	}
}
