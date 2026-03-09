package configutil

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestIntEnvWithDefaultStrict(t *testing.T) {
	t.Setenv("TEST_INT_ENV", "")

	value, err := IntEnvWithDefaultStrict("TEST_INT_ENV", 7)
	if err != nil {
		t.Fatalf("unexpected error for empty env: %v", err)
	}
	if value != 7 {
		t.Fatalf("expected fallback value 7, got %d", value)
	}

	t.Setenv("TEST_INT_ENV", "42")
	value, err = IntEnvWithDefaultStrict("TEST_INT_ENV", 7)
	if err != nil {
		t.Fatalf("unexpected error for valid env: %v", err)
	}
	if value != 42 {
		t.Fatalf("expected parsed value 42, got %d", value)
	}

	t.Setenv("TEST_INT_ENV", "oops")
	_, err = IntEnvWithDefaultStrict("TEST_INT_ENV", 7)
	if err == nil {
		t.Fatal("expected error for invalid env")
	}
	if !strings.Contains(err.Error(), "invalid TEST_INT_ENV") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBoolEnvWithDefaultStrict(t *testing.T) {
	t.Setenv("TEST_BOOL_ENV", "")

	value, err := BoolEnvWithDefaultStrict("TEST_BOOL_ENV", true)
	if err != nil {
		t.Fatalf("unexpected error for empty env: %v", err)
	}
	if !value {
		t.Fatalf("expected fallback value true, got %t", value)
	}

	t.Setenv("TEST_BOOL_ENV", "false")
	value, err = BoolEnvWithDefaultStrict("TEST_BOOL_ENV", true)
	if err != nil {
		t.Fatalf("unexpected error for valid env: %v", err)
	}
	if value {
		t.Fatalf("expected parsed value false, got %t", value)
	}

	t.Setenv("TEST_BOOL_ENV", "not-bool")
	_, err = BoolEnvWithDefaultStrict("TEST_BOOL_ENV", true)
	if err == nil {
		t.Fatal("expected error for invalid env")
	}
	if !strings.Contains(err.Error(), "invalid TEST_BOOL_ENV") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseCSV(t *testing.T) {
	values := ParseCSV(" alpha, ,beta,gamma ,, ")
	expected := []string{"alpha", "beta", "gamma"}
	if !reflect.DeepEqual(values, expected) {
		t.Fatalf("expected %v, got %v", expected, values)
	}
}

func TestParseWeekday(t *testing.T) {
	weekday, err := ParseWeekday(" Tue ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if weekday != time.Tuesday {
		t.Fatalf("expected Tuesday, got %v", weekday)
	}

	_, err = ParseWeekday("funday")
	if err == nil {
		t.Fatal("expected error for invalid weekday")
	}
	if !strings.Contains(err.Error(), "expected one of") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadNotifyConfig(t *testing.T) {
	t.Setenv("TEST_NOTIFY_URL", "http://localhost:8080/notify")
	t.Setenv("TEST_NOTIFY_TOKEN", "token")
	t.Setenv("TEST_NOTIFY_CHANNEL", "alerts")
	t.Setenv("TEST_NOTIFY_SEVERITY", " WARNING ")

	cfg, err := LoadNotifyConfig(
		"TEST_NOTIFY_URL",
		"TEST_NOTIFY_TOKEN",
		"TEST_NOTIFY_CHANNEL",
		"TEST_NOTIFY_SEVERITY",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.URL != "http://localhost:8080/notify" {
		t.Fatalf("unexpected URL: %q", cfg.URL)
	}
	if cfg.AuthToken != "token" {
		t.Fatalf("unexpected AuthToken: %q", cfg.AuthToken)
	}
	if cfg.Channel != "alerts" {
		t.Fatalf("unexpected Channel: %q", cfg.Channel)
	}
	if cfg.Severity != "warning" {
		t.Fatalf("unexpected Severity: %q", cfg.Severity)
	}
}

func TestLoadNotifyConfigInvalidSeverity(t *testing.T) {
	t.Setenv("TEST_NOTIFY_URL", "http://localhost:8080/notify")
	t.Setenv("TEST_NOTIFY_TOKEN", "token")
	t.Setenv("TEST_NOTIFY_CHANNEL", "alerts")
	t.Setenv("TEST_NOTIFY_SEVERITY", "urgent")

	_, err := LoadNotifyConfig(
		"TEST_NOTIFY_URL",
		"TEST_NOTIFY_TOKEN",
		"TEST_NOTIFY_CHANNEL",
		"TEST_NOTIFY_SEVERITY",
	)
	if err == nil {
		t.Fatal("expected invalid severity error")
	}
	if !strings.Contains(err.Error(), "TEST_NOTIFY_SEVERITY") {
		t.Fatalf("unexpected error: %v", err)
	}
}
