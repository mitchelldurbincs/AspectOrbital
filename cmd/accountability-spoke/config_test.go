package main

import (
	"strings"
	"testing"
)

func clearAccountabilityEnv(t *testing.T) {
	t.Helper()

	keys := []string{
		"ACCOUNTABILITY_SPOKE_HTTP_ADDR",
		"ACCOUNTABILITY_DB_PATH",
		"ACCOUNTABILITY_EXPIRY_POLL_INTERVAL",
		"ACCOUNTABILITY_COMMAND_COMMIT",
		"ACCOUNTABILITY_COMMAND_PROOF",
		"ACCOUNTABILITY_COMMAND_STATUS",
		"ACCOUNTABILITY_COMMAND_CANCEL",
		"BEEMINDER_API_BASE_URL",
		"BEEMINDER_AUTH_TOKEN",
		"BEEMINDER_USERNAME",
	}

	for _, key := range keys {
		t.Setenv(key, "")
	}
}

func setAccountabilityRequiredEnv(t *testing.T) {
	t.Helper()

	t.Setenv("ACCOUNTABILITY_SPOKE_HTTP_ADDR", "127.0.0.1:8093")
	t.Setenv("ACCOUNTABILITY_DB_PATH", "file:accountability.db?_pragma=busy_timeout(5000)")
	t.Setenv("ACCOUNTABILITY_EXPIRY_POLL_INTERVAL", "45s")
	t.Setenv("ACCOUNTABILITY_COMMAND_COMMIT", "commit")
	t.Setenv("ACCOUNTABILITY_COMMAND_PROOF", "proof")
	t.Setenv("ACCOUNTABILITY_COMMAND_STATUS", "status")
	t.Setenv("ACCOUNTABILITY_COMMAND_CANCEL", "cancel")
	t.Setenv("BEEMINDER_API_BASE_URL", "https://www.beeminder.com/api/v1")
	t.Setenv("BEEMINDER_AUTH_TOKEN", "token")
	t.Setenv("BEEMINDER_USERNAME", "username")
}

func TestLoadConfigRequiresHTTPAddr(t *testing.T) {
	clearAccountabilityEnv(t)
	setAccountabilityRequiredEnv(t)
	t.Setenv("ACCOUNTABILITY_SPOKE_HTTP_ADDR", "")

	_, err := loadConfig()
	if err == nil {
		t.Fatal("expected error for missing ACCOUNTABILITY_SPOKE_HTTP_ADDR")
	}
	if !strings.Contains(err.Error(), "ACCOUNTABILITY_SPOKE_HTTP_ADDR is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfigRequiresBeeminderAuthToken(t *testing.T) {
	clearAccountabilityEnv(t)
	setAccountabilityRequiredEnv(t)
	t.Setenv("BEEMINDER_AUTH_TOKEN", "")

	_, err := loadConfig()
	if err == nil {
		t.Fatal("expected error for missing BEEMINDER_AUTH_TOKEN")
	}
	if !strings.Contains(err.Error(), "BEEMINDER_AUTH_TOKEN is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}
