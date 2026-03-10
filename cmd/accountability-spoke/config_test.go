package main

import (
	"os"
	"strings"
	"testing"
)

func clearAccountabilityEnv(t *testing.T) {
	t.Helper()

	keys := []string{
		"ACCOUNTABILITY_SPOKE_HTTP_ADDR",
		"ACCOUNTABILITY_DB_PATH",
		"ACCOUNTABILITY_EXPIRY_POLL_INTERVAL",
		"ACCOUNTABILITY_EXPIRY_GRACE_PERIOD",
		"ACCOUNTABILITY_REMINDER_INTERVAL",
		"HUB_NOTIFY_URL",
		"HUB_NOTIFY_AUTH_TOKEN",
		"ACCOUNTABILITY_NOTIFY_CHANNEL",
		"ACCOUNTABILITY_NOTIFY_SEVERITY",
		"ACCOUNTABILITY_POLICY_FILE",
		"ACCOUNTABILITY_DEFAULT_SNOOZE",
		"ACCOUNTABILITY_MAX_SNOOZE",
		"ACCOUNTABILITY_COMMAND_COMMIT",
		"ACCOUNTABILITY_COMMAND_PROOF",
		"ACCOUNTABILITY_COMMAND_STATUS",
		"ACCOUNTABILITY_COMMAND_CANCEL",
		"ACCOUNTABILITY_COMMAND_SNOOZE",
	}

	for _, key := range keys {
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("failed to unset %s: %v", key, err)
		}
	}
}

func setAccountabilityRequiredEnv(t *testing.T) {
	t.Helper()

	t.Setenv("ACCOUNTABILITY_SPOKE_HTTP_ADDR", "127.0.0.1:8093")
	t.Setenv("ACCOUNTABILITY_DB_PATH", "file:accountability.db?_pragma=busy_timeout(5000)")
	t.Setenv("ACCOUNTABILITY_EXPIRY_POLL_INTERVAL", "45s")
	t.Setenv("ACCOUNTABILITY_EXPIRY_GRACE_PERIOD", "12h")
	t.Setenv("ACCOUNTABILITY_REMINDER_INTERVAL", "5m")
	t.Setenv("HUB_NOTIFY_URL", "http://127.0.0.1:8080/notify")
	t.Setenv("HUB_NOTIFY_AUTH_TOKEN", "test-notify-token")
	t.Setenv("ACCOUNTABILITY_NOTIFY_CHANNEL", "accountability")
	t.Setenv("ACCOUNTABILITY_NOTIFY_SEVERITY", "warning")
	t.Setenv("ACCOUNTABILITY_POLICY_FILE", "cmd/accountability-spoke/policies.example.json")
	t.Setenv("ACCOUNTABILITY_DEFAULT_SNOOZE", "10m")
	t.Setenv("ACCOUNTABILITY_MAX_SNOOZE", "60m")
	t.Setenv("ACCOUNTABILITY_COMMAND_COMMIT", "commit")
	t.Setenv("ACCOUNTABILITY_COMMAND_PROOF", "proof")
	t.Setenv("ACCOUNTABILITY_COMMAND_STATUS", "status")
	t.Setenv("ACCOUNTABILITY_COMMAND_CANCEL", "cancel")
	t.Setenv("ACCOUNTABILITY_COMMAND_SNOOZE", "a-snooze")
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

func TestLoadConfigRejectsNonPositiveExpiryPollInterval(t *testing.T) {
	clearAccountabilityEnv(t)
	setAccountabilityRequiredEnv(t)
	t.Setenv("ACCOUNTABILITY_EXPIRY_POLL_INTERVAL", "0s")

	_, err := loadConfig()
	if err == nil {
		t.Fatal("expected error for non-positive ACCOUNTABILITY_EXPIRY_POLL_INTERVAL")
	}
	if !strings.Contains(err.Error(), "ACCOUNTABILITY_EXPIRY_POLL_INTERVAL must be positive") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfigRejectsDefaultSnoozeAboveMax(t *testing.T) {
	clearAccountabilityEnv(t)
	setAccountabilityRequiredEnv(t)
	t.Setenv("ACCOUNTABILITY_DEFAULT_SNOOZE", "90m")
	t.Setenv("ACCOUNTABILITY_MAX_SNOOZE", "60m")

	_, err := loadConfig()
	if err == nil {
		t.Fatal("expected error when ACCOUNTABILITY_DEFAULT_SNOOZE exceeds ACCOUNTABILITY_MAX_SNOOZE")
	}
	if !strings.Contains(err.Error(), "ACCOUNTABILITY_DEFAULT_SNOOZE cannot exceed ACCOUNTABILITY_MAX_SNOOZE") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfigRejectsDuplicateNormalizedCommandNames(t *testing.T) {
	clearAccountabilityEnv(t)
	setAccountabilityRequiredEnv(t)
	t.Setenv("ACCOUNTABILITY_COMMAND_COMMIT", "commit")
	t.Setenv("ACCOUNTABILITY_COMMAND_STATUS", " Commit ")

	_, err := loadConfig()
	if err == nil {
		t.Fatal("expected error for duplicate normalized command names")
	}
	if !strings.Contains(err.Error(), "both normalize to \"commit\"") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfigRejectsInvalidNormalizedCommandName(t *testing.T) {
	clearAccountabilityEnv(t)
	setAccountabilityRequiredEnv(t)
	t.Setenv("ACCOUNTABILITY_COMMAND_PROOF", "bad name")

	_, err := loadConfig()
	if err == nil {
		t.Fatal("expected error for invalid normalized command name")
	}
	if !strings.Contains(err.Error(), "ACCOUNTABILITY_COMMAND_PROOF is invalid after normalization") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfigNormalizesValidCommandNames(t *testing.T) {
	clearAccountabilityEnv(t)
	setAccountabilityRequiredEnv(t)
	t.Setenv("ACCOUNTABILITY_COMMAND_COMMIT", " Commit ")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("expected config to load, got: %v", err)
	}
	if cfg.CommitCommandName != "commit" {
		t.Fatalf("expected normalized command name 'commit', got %q", cfg.CommitCommandName)
	}
}
