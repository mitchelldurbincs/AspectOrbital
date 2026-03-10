package main

import (
	"strings"
	"testing"
	"time"

	"personal-infrastructure/pkg/accountability"
)

func TestFormatReminderMessageEscalates(t *testing.T) {
	cfg := config{ProofCommandName: "proof", SnoozeCommandName: "a-snooze", CheckInCommandName: "checkin"}
	commitment := accountability.Commitment{
		UserID:          "u1",
		Task:            "gym",
		Deadline:        time.Date(2026, time.March, 10, 10, 0, 0, 0, time.UTC),
		LastCheckInAt:   time.Date(2026, time.March, 10, 10, 5, 0, 0, time.UTC),
		LastCheckInText: "getting ready",
	}

	first := formatReminderMessage(cfg, commitment)
	if !strings.Contains(first, "/checkin") || !strings.Contains(first, "/a-snooze") {
		t.Fatalf("expected first reminder to mention check-in and snooze, got %q", first)
	}

	commitment.ReminderCount = 1
	second := formatReminderMessage(cfg, commitment)
	if !strings.Contains(second, "still have not finished") || !strings.Contains(second, "Last check-in") {
		t.Fatalf("expected firmer second reminder with check-in context, got %q", second)
	}

	commitment.ReminderCount = 2
	third := formatReminderMessage(cfg, commitment)
	if !strings.Contains(third, "at risk of being missed") {
		t.Fatalf("expected strongest escalation on later reminders, got %q", third)
	}
}
