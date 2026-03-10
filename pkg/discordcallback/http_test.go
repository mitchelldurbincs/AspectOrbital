package discordcallback

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"personal-infrastructure/pkg/hubnotify"
)

func TestIsAuthorized(t *testing.T) {
	tests := []struct {
		name      string
		header    string
		token     string
		wantValid bool
	}{
		{name: "missing header", token: "secret", wantValid: false},
		{name: "wrong scheme", header: "Basic secret", token: "secret", wantValid: false},
		{name: "wrong token", header: "Bearer wrong", token: "secret", wantValid: false},
		{name: "correct token", header: "Bearer secret", token: "secret", wantValid: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/discord/callback", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			if got := IsAuthorized(req, tt.token); got != tt.wantValid {
				t.Fatalf("IsAuthorized() = %v, want %v", got, tt.wantValid)
			}
		})
	}
}

func TestHandleHTTP(t *testing.T) {
	validBody := `{"version":2,"service":"svc","event":"evt","action":{"id":"go"},"context":{"discordUserId":"u1"}}`

	tests := []struct {
		name       string
		method     string
		header     string
		body       string
		handler    HandlerFunc
		wantStatus int
		wantBody   string
	}{
		{name: "wrong method", method: http.MethodGet, wantStatus: http.StatusMethodNotAllowed, wantBody: "method not allowed"},
		{name: "unauthorized", method: http.MethodPost, body: validBody, wantStatus: http.StatusUnauthorized, wantBody: "unauthorized"},
		{name: "bad json", method: http.MethodPost, header: "Bearer secret", body: `{`, wantStatus: http.StatusBadRequest},
		{name: "validation error", method: http.MethodPost, header: "Bearer secret", body: `{"version":2,"service":"wrong","event":"evt","action":{"id":"go"},"context":{"discordUserId":"u1"}}`, wantStatus: http.StatusBadRequest, wantBody: "service must be svc"},
		{name: "handler error", method: http.MethodPost, header: "Bearer secret", body: validBody, handler: func(_ *http.Request, _ hubnotify.ActionCallbackRequest) (hubnotify.ActionCallbackResponse, int, error) {
			return hubnotify.ActionCallbackResponse{}, http.StatusConflict, errors.New("nope")
		}, wantStatus: http.StatusConflict, wantBody: "nope"},
		{name: "success", method: http.MethodPost, header: "Bearer secret", body: validBody, handler: func(_ *http.Request, payload hubnotify.ActionCallbackRequest) (hubnotify.ActionCallbackResponse, int, error) {
			if payload.Action.ID != "go" {
				t.Fatalf("unexpected payload: %#v", payload)
			}
			return hubnotify.ActionCallbackResponse{Status: "ok", Message: "done"}, http.StatusOK, nil
		}, wantStatus: http.StatusOK, wantBody: `"message":"done"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/discord/callback", strings.NewReader(tt.body))
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rec := httptest.NewRecorder()
			handler := tt.handler
			if handler == nil {
				handler = func(_ *http.Request, _ hubnotify.ActionCallbackRequest) (hubnotify.ActionCallbackResponse, int, error) {
					return hubnotify.ActionCallbackResponse{Status: "ok", Message: "done"}, http.StatusOK, nil
				}
			}

			HandleHTTP(rec, req, Options{AuthToken: "secret", ExpectedService: "svc", ExpectedEvent: "evt"}, handler)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d body=%q", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantBody != "" && !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Fatalf("body %q does not contain %q", rec.Body.String(), tt.wantBody)
			}
		})
	}
}
