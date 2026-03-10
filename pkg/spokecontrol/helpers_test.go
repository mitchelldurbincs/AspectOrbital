package spokecontrol

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateDiscordUserRequiresDiscordUserID(t *testing.T) {
	req := Request{}

	err := ValidateDiscordUser(req)
	if err == nil || err.Error() != "context.discordUserId is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNormalizeCommandPreservesExactValue(t *testing.T) {
	req := Request{Command: "  Finance-Status  "}

	if got := NormalizeCommand(req); got != "  Finance-Status  " {
		t.Fatalf("unexpected command value: %q", got)
	}
}

func TestOKIncludesDataWhenPresent(t *testing.T) {
	payload := OK("finance-status", "all good", map[string]any{"enabled": true})

	if payload["status"] != "ok" || payload["command"] != "finance-status" || payload["message"] != "all good" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	if _, ok := payload["data"]; !ok {
		t.Fatalf("expected data field in payload: %#v", payload)
	}
}

func TestOKOmitsDataWhenNil(t *testing.T) {
	payload := OK("finance-status", "all good", nil)

	if _, ok := payload["data"]; ok {
		t.Fatalf("did not expect data field in payload: %#v", payload)
	}
}

func TestUnknownCommandErrorSortsValidCommands(t *testing.T) {
	err := UnknownCommandError("wat", []string{"zeta", "alpha"})
	if want := `unknown command "wat"; valid commands: alpha, zeta`; err != want {
		t.Fatalf("unexpected error: got %q want %q", err, want)
	}
}

func TestIsAuthorizedRequiresMatchingBearerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/control/command", nil)
	if IsAuthorized(req, "test-token") {
		t.Fatal("expected request without auth header to be rejected")
	}

	req.Header.Set("Authorization", "Bearer wrong-token")
	if IsAuthorized(req, "test-token") {
		t.Fatal("expected mismatched token to be rejected")
	}

	req.Header.Set("Authorization", "Bearer test-token")
	if !IsAuthorized(req, "test-token") {
		t.Fatal("expected matching bearer token to be accepted")
	}
}
