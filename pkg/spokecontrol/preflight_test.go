package spokecontrol

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPreflightCommandRejectsWrongMethod(t *testing.T) {
	var reqPayload Request
	req := httptest.NewRequest(http.MethodGet, "/control/command", nil)

	result := PreflightCommand(req, "test-token", &reqPayload, func() Request { return reqPayload })

	if !result.Failed() || result.StatusCode != http.StatusMethodNotAllowed || result.Err.Error() != "method not allowed" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestPreflightCommandRejectsUnauthorizedRequest(t *testing.T) {
	var reqPayload Request
	req := httptest.NewRequest(http.MethodPost, "/control/command", strings.NewReader(`{"command":"status","context":{"discordUserId":"u1"}}`))

	result := PreflightCommand(req, "test-token", &reqPayload, func() Request { return reqPayload })

	if !result.Failed() || result.StatusCode != http.StatusUnauthorized || result.Err.Error() != "unauthorized" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestPreflightCommandRejectsMissingDiscordUserID(t *testing.T) {
	var reqPayload Request
	req := httptest.NewRequest(http.MethodPost, "/control/command", strings.NewReader(`{"command":"status","context":{}}`))
	req.Header.Set("Authorization", "Bearer test-token")

	result := PreflightCommand(req, "test-token", &reqPayload, func() Request { return reqPayload })

	if !result.Failed() || result.StatusCode != http.StatusBadRequest || result.Err.Error() != "context.discordUserId is required" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestPreflightCommandDecodesPayloadWhenValid(t *testing.T) {
	var reqPayload Request
	req := httptest.NewRequest(http.MethodPost, "/control/command", strings.NewReader(`{"command":"status","context":{"discordUserId":"u1"}}`))
	req.Header.Set("Authorization", "Bearer test-token")

	result := PreflightCommand(req, "test-token", &reqPayload, func() Request { return reqPayload })

	if result != nil {
		t.Fatalf("expected nil result, got %#v", result)
	}
	if reqPayload.Command != "status" || reqPayload.Context.DiscordUserID != "u1" {
		t.Fatalf("unexpected decoded payload: %#v", reqPayload)
	}
}
