package configutil

import (
	"strings"
	"testing"
	"time"
)

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

func TestNormalizeSeverity(t *testing.T) {
	if s := NormalizeSeverity("  WARNING  "); s != "warning" {
		t.Fatalf("expected warning, got %q", s)
	}
}

func TestValidateSeverity(t *testing.T) {
	if err := ValidateSeverity("warning", nil); err != nil {
		t.Fatalf("unexpected error for valid severity: %v", err)
	}

	err := ValidateSeverity("urgent", nil)
	if err == nil {
		t.Fatal("expected error for invalid severity")
	}
	if !strings.Contains(err.Error(), "must be one of") {
		t.Fatalf("unexpected error message: %v", err)
	}
}
