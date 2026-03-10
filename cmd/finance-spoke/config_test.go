package main

import (
	"os"
	"strings"
	"testing"
)

func clearFinanceEnv(t *testing.T) {
	t.Helper()

	keys := []string{
		"FINANCE_SPOKE_HTTP_ADDR",
		"FINANCE_HUB_NOTIFY_URL",
		"FINANCE_HUB_NOTIFY_AUTH_TOKEN",
		"FINANCE_NOTIFY_CHANNEL",
		"FINANCE_NOTIFY_SEVERITY",
		"FINANCE_SUMMARY_ENABLED",
		"FINANCE_SUMMARY_TITLE",
		"FINANCE_SUMMARY_WEEKDAY",
		"FINANCE_SUMMARY_TIME",
		"FINANCE_SUMMARY_TIMEZONE",
		"FINANCE_SUMMARY_LOOKBACK_DAYS",
		"FINANCE_SUMMARY_SEND_EMPTY",
		"FINANCE_SUMMARY_MAX_ITEMS",
		"FINANCE_SUMMARY_POLL_INTERVAL",
		"FINANCE_STATE_FILE",
		"PLAID_CLIENT_ID",
		"PLAID_SECRET",
		"PLAID_ENV",
		"PLAID_ACCESS_TOKENS",
		"PLAID_CLIENT_NAME",
		"PLAID_COUNTRY_CODES",
		"PLAID_LANGUAGE",
		"PLAID_WEBHOOK_URL",
		"FINANCE_HTTP_TIMEOUT",
	}

	for _, key := range keys {
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("failed to unset %s: %v", key, err)
		}
	}
}

func setFinanceRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("FINANCE_SPOKE_HTTP_ADDR", "127.0.0.1:8091")
	t.Setenv("FINANCE_HUB_NOTIFY_URL", "http://127.0.0.1:8080/notify")
	t.Setenv("FINANCE_HUB_NOTIFY_AUTH_TOKEN", "test-notify-token")
	t.Setenv("FINANCE_NOTIFY_CHANNEL", "finance-summary")
	t.Setenv("FINANCE_NOTIFY_SEVERITY", "info")
	t.Setenv("FINANCE_SUMMARY_ENABLED", "true")
	t.Setenv("FINANCE_SUMMARY_TITLE", "Weekly Subscription Summary")
	t.Setenv("FINANCE_SUMMARY_WEEKDAY", "SUN")
	t.Setenv("FINANCE_SUMMARY_TIME", "18:00")
	t.Setenv("FINANCE_SUMMARY_TIMEZONE", "America/New_York")
	t.Setenv("FINANCE_SUMMARY_LOOKBACK_DAYS", "7")
	t.Setenv("FINANCE_SUMMARY_SEND_EMPTY", "false")
	t.Setenv("FINANCE_SUMMARY_MAX_ITEMS", "20")
	t.Setenv("FINANCE_SUMMARY_POLL_INTERVAL", "1m")
	t.Setenv("FINANCE_STATE_FILE", "var/finance-spoke/state.json")
	t.Setenv("PLAID_CLIENT_ID", "cid")
	t.Setenv("PLAID_SECRET", "secret")
	t.Setenv("PLAID_ENV", "production")
	t.Setenv("PLAID_ACCESS_TOKENS", "tok")
	t.Setenv("PLAID_CLIENT_NAME", "Aspect Orbital Finance")
	t.Setenv("PLAID_COUNTRY_CODES", "US")
	t.Setenv("PLAID_LANGUAGE", "en")
	t.Setenv("PLAID_WEBHOOK_URL", "https://example.com/webhook")
	t.Setenv("FINANCE_HTTP_TIMEOUT", "15s")
}

func TestLoadConfigInvalidSummaryEnabledFails(t *testing.T) {
	clearFinanceEnv(t)
	setFinanceRequiredEnv(t)
	t.Setenv("FINANCE_SUMMARY_ENABLED", "sometimes")

	_, err := loadConfig()
	if err == nil {
		t.Fatal("expected error for invalid FINANCE_SUMMARY_ENABLED")
	}
	if !strings.Contains(err.Error(), "assigning FINANCE_SUMMARY_ENABLED") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfigInvalidSummaryLookbackDaysFails(t *testing.T) {
	clearFinanceEnv(t)
	setFinanceRequiredEnv(t)
	t.Setenv("FINANCE_SUMMARY_LOOKBACK_DAYS", "seven")

	_, err := loadConfig()
	if err == nil {
		t.Fatal("expected error for invalid FINANCE_SUMMARY_LOOKBACK_DAYS")
	}
	if !strings.Contains(err.Error(), "assigning FINANCE_SUMMARY_LOOKBACK_DAYS") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfigInvalidSummarySendEmptyFails(t *testing.T) {
	clearFinanceEnv(t)
	setFinanceRequiredEnv(t)
	t.Setenv("FINANCE_SUMMARY_SEND_EMPTY", "maybe")

	_, err := loadConfig()
	if err == nil {
		t.Fatal("expected error for invalid FINANCE_SUMMARY_SEND_EMPTY")
	}
	if !strings.Contains(err.Error(), "assigning FINANCE_SUMMARY_SEND_EMPTY") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfigInvalidSummaryMaxItemsFails(t *testing.T) {
	clearFinanceEnv(t)
	setFinanceRequiredEnv(t)
	t.Setenv("FINANCE_SUMMARY_MAX_ITEMS", "many")

	_, err := loadConfig()
	if err == nil {
		t.Fatal("expected error for invalid FINANCE_SUMMARY_MAX_ITEMS")
	}
	if !strings.Contains(err.Error(), "assigning FINANCE_SUMMARY_MAX_ITEMS") {
		t.Fatalf("unexpected error: %v", err)
	}
}
