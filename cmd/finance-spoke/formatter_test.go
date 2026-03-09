package main

import (
	"strings"
	"testing"
	"time"
)

func TestRenderWeeklySummaryMessage(t *testing.T) {
	loc := time.UTC
	summary := weeklySummary{
		WeekKey:     "2026-01-05",
		WindowStart: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
		WindowEnd:   time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC),
		Charges:     []subscriptionCharge{{Merchant: "Netflix", Amount: 19.99, OccurredAt: time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC), AccountLabel: "Visa"}, {Merchant: "", Amount: 10, OccurredAt: time.Date(2026, 1, 7, 0, 0, 0, 0, time.UTC)}},
		TotalAmount: 29.99,
	}

	got := renderWeeklySummaryMessage("Weekly Subs", summary, loc, 1)
	checks := []string{"Weekly Subs (Jan 5 - Jan 11)", "- Netflix: $19.99 on Jan 6 (Visa)", "- ...and 1 more", "Total: $29.99 across 2 unique subscriptions"}
	for _, c := range checks {
		if !strings.Contains(got, c) {
			t.Fatalf("message %q missing expected fragment %q", got, c)
		}
	}

	empty := weeklySummary{WindowStart: summary.WindowStart, WindowEnd: summary.WindowEnd}
	emptyMsg := renderWeeklySummaryMessage("Weekly Subs", empty, loc, 10)
	if !strings.Contains(emptyMsg, "No subscription charges") {
		t.Fatalf("expected empty message, got %q", emptyMsg)
	}
}
