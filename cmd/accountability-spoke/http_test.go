package main

import (
	"testing"
	"time"
)

func TestParseDeadlineClockTimeRollsToNextDay(t *testing.T) {
	now := time.Date(2026, time.March, 9, 20, 0, 0, 0, time.UTC)
	deadline, err := parseDeadline(now, "4:30am")
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, time.March, 10, 4, 30, 0, 0, time.UTC)
	if !deadline.Equal(want) {
		t.Fatalf("unexpected deadline\nwant: %s\ngot:  %s", want.Format(time.RFC3339), deadline.Format(time.RFC3339))
	}
}

func TestParseDeadlineClockTimeUsesSameDayWhenFuture(t *testing.T) {
	now := time.Date(2026, time.March, 9, 3, 0, 0, 0, time.UTC)
	deadline, err := parseDeadline(now, "4:30am")
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, time.March, 9, 4, 30, 0, 0, time.UTC)
	if !deadline.Equal(want) {
		t.Fatalf("unexpected deadline\nwant: %s\ngot:  %s", want.Format(time.RFC3339), deadline.Format(time.RFC3339))
	}
}
