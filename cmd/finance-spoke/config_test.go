package main

import (
	"strings"
	"testing"
)

func clearFinanceEnv(t *testing.T) {
	t.Helper()

	keys := []string{
		"FINANCE_SPOKE_HTTP_ADDR",
		"FINANCE_HUB_NOTIFY_URL",
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
		t.Setenv(key, "")
	}
}

func TestLoadConfigInvalidSummaryEnabledFails(t *testing.T) {
	clearFinanceEnv(t)
	t.Setenv("FINANCE_SUMMARY_ENABLED", "sometimes")

	_, err := loadConfig()
	if err == nil {
		t.Fatal("expected error for invalid FINANCE_SUMMARY_ENABLED")
	}
	if !strings.Contains(err.Error(), "invalid FINANCE_SUMMARY_ENABLED") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfigInvalidSummaryLookbackDaysFails(t *testing.T) {
	clearFinanceEnv(t)
	t.Setenv("FINANCE_SUMMARY_LOOKBACK_DAYS", "seven")

	_, err := loadConfig()
	if err == nil {
		t.Fatal("expected error for invalid FINANCE_SUMMARY_LOOKBACK_DAYS")
	}
	if !strings.Contains(err.Error(), "invalid FINANCE_SUMMARY_LOOKBACK_DAYS") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfigInvalidSummarySendEmptyFails(t *testing.T) {
	clearFinanceEnv(t)
	t.Setenv("FINANCE_SUMMARY_SEND_EMPTY", "maybe")

	_, err := loadConfig()
	if err == nil {
		t.Fatal("expected error for invalid FINANCE_SUMMARY_SEND_EMPTY")
	}
	if !strings.Contains(err.Error(), "invalid FINANCE_SUMMARY_SEND_EMPTY") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfigInvalidSummaryMaxItemsFails(t *testing.T) {
	clearFinanceEnv(t)
	t.Setenv("FINANCE_SUMMARY_MAX_ITEMS", "many")

	_, err := loadConfig()
	if err == nil {
		t.Fatal("expected error for invalid FINANCE_SUMMARY_MAX_ITEMS")
	}
	if !strings.Contains(err.Error(), "invalid FINANCE_SUMMARY_MAX_ITEMS") {
		t.Fatalf("unexpected error: %v", err)
	}
}
