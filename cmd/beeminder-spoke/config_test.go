package main

import (
	"os"
	"strings"
	"testing"
)

func clearBeeminderEnv(t *testing.T) {
	t.Helper()

	keys := []string{
		"BEEMINDER_SPOKE_HTTP_ADDR",
		"BEEMINDER_API_BASE_URL",
		"BEEMINDER_AUTH_TOKEN",
		"BEEMINDER_USERNAME",
		"BEEMINDER_GOAL_SLUGS",
		"BEEMINDER_GOAL_SLUG",
		"HUB_NOTIFY_URL",
		"HUB_NOTIFY_AUTH_TOKEN",
		"SPOKE_COMMAND_AUTH_TOKEN",
		"BEEMINDER_DISCORD_CALLBACK_URL",
		"BEEMINDER_CALLBACK_AUTH_TOKEN",
		"BEEMINDER_NOTIFY_CHANNEL",
		"BEEMINDER_NOTIFY_SEVERITY",
		"BEEMINDER_POLL_INTERVAL",
		"BEEMINDER_REMINDER_INTERVAL",
		"BEEMINDER_REMINDER_START",
		"BEEMINDER_ACTIVE_GRACE",
		"BEEMINDER_STARTED_SNOOZE",
		"BEEMINDER_DEFAULT_SNOOZE",
		"BEEMINDER_MAX_SNOOZE",
		"BEEMINDER_REQUIRE_DAILY_RATE",
		"BEEMINDER_HTTP_TIMEOUT",
		"BEEMINDER_DATAPOINTS_PER_PAGE",
		"BEEMINDER_MAX_DATAPOINT_PAGES",
	}

	for _, key := range keys {
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("failed to unset %s: %v", key, err)
		}
	}
}

func setBeeminderRequiredEnv(t *testing.T) {
	t.Helper()

	t.Setenv("BEEMINDER_SPOKE_HTTP_ADDR", "127.0.0.1:8090")
	t.Setenv("BEEMINDER_API_BASE_URL", "https://www.beeminder.com/api/v1")
	t.Setenv("BEEMINDER_AUTH_TOKEN", "test-auth-token")
	t.Setenv("BEEMINDER_USERNAME", "test-user")
	t.Setenv("BEEMINDER_GOAL_SLUGS", "study")
	t.Setenv("HUB_NOTIFY_URL", "http://127.0.0.1:8080/notify")
	t.Setenv("HUB_NOTIFY_AUTH_TOKEN", "test-notify-token")
	t.Setenv("SPOKE_COMMAND_AUTH_TOKEN", "test-command-token")
	t.Setenv("BEEMINDER_DISCORD_CALLBACK_URL", "http://127.0.0.1:8090/discord/callback")
	t.Setenv("BEEMINDER_CALLBACK_AUTH_TOKEN", "test-callback-token")
	t.Setenv("BEEMINDER_NOTIFY_CHANNEL", "beeminder")
	t.Setenv("BEEMINDER_NOTIFY_SEVERITY", "warning")
	t.Setenv("BEEMINDER_POLL_INTERVAL", "1m")
	t.Setenv("BEEMINDER_REMINDER_INTERVAL", "5m")
	t.Setenv("BEEMINDER_REMINDER_START", "19:00")
	t.Setenv("BEEMINDER_ACTIVE_GRACE", "20m")
	t.Setenv("BEEMINDER_STARTED_SNOOZE", "30m")
	t.Setenv("BEEMINDER_DEFAULT_SNOOZE", "30m")
	t.Setenv("BEEMINDER_MAX_SNOOZE", "2h")
	t.Setenv("BEEMINDER_REQUIRE_DAILY_RATE", "true")
	t.Setenv("BEEMINDER_HTTP_TIMEOUT", "10s")
	t.Setenv("BEEMINDER_DATAPOINTS_PER_PAGE", "100")
	t.Setenv("BEEMINDER_MAX_DATAPOINT_PAGES", "20")
}

func TestLoadConfigRequiresBeeminderGoalSlugs(t *testing.T) {
	clearBeeminderEnv(t)
	setBeeminderRequiredEnv(t)
	if err := os.Unsetenv("BEEMINDER_GOAL_SLUGS"); err != nil {
		t.Fatalf("failed to unset BEEMINDER_GOAL_SLUGS: %v", err)
	}
	t.Setenv("BEEMINDER_GOAL_SLUG", "legacy-goal")

	_, err := loadConfig()
	if err == nil {
		t.Fatal("expected error for missing BEEMINDER_GOAL_SLUGS")
	}
	if !strings.Contains(err.Error(), "required key BEEMINDER_GOAL_SLUGS missing value") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfigRequiresBeeminderMaxSnooze(t *testing.T) {
	clearBeeminderEnv(t)
	setBeeminderRequiredEnv(t)
	if err := os.Unsetenv("BEEMINDER_MAX_SNOOZE"); err != nil {
		t.Fatalf("failed to unset BEEMINDER_MAX_SNOOZE: %v", err)
	}

	_, err := loadConfig()
	if err == nil {
		t.Fatal("expected error for missing BEEMINDER_MAX_SNOOZE")
	}
	if !strings.Contains(err.Error(), "required key BEEMINDER_MAX_SNOOZE missing value") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfigRequiresBeeminderRequireDailyRate(t *testing.T) {
	clearBeeminderEnv(t)
	setBeeminderRequiredEnv(t)
	if err := os.Unsetenv("BEEMINDER_REQUIRE_DAILY_RATE"); err != nil {
		t.Fatalf("failed to unset BEEMINDER_REQUIRE_DAILY_RATE: %v", err)
	}

	_, err := loadConfig()
	if err == nil {
		t.Fatal("expected error for missing BEEMINDER_REQUIRE_DAILY_RATE")
	}
	if !strings.Contains(err.Error(), "required key BEEMINDER_REQUIRE_DAILY_RATE missing value") {
		t.Fatalf("unexpected error: %v", err)
	}
}
