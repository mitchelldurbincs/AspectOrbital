package configutil

import (
	"strings"
	"testing"
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
