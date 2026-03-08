package main

import (
	"fmt"
	"strings"
	"time"
)

func renderWeeklySummaryMessage(title string, summary weeklySummary, location *time.Location, maxItems int) string {
	if maxItems <= 0 {
		maxItems = len(summary.Charges)
	}

	windowStart := inLocation(summary.WindowStart, location)
	windowEnd := inLocation(summary.WindowEnd.Add(-time.Nanosecond), location)

	b := &strings.Builder{}
	fmt.Fprintf(b, "%s (%s - %s)\n", title, windowStart.Format("Jan 2"), windowEnd.Format("Jan 2"))

	if len(summary.Charges) == 0 {
		b.WriteString("- No subscription charges detected this week.\n")
		return strings.TrimSpace(b.String())
	}

	shown := len(summary.Charges)
	if shown > maxItems {
		shown = maxItems
	}

	for idx := 0; idx < shown; idx++ {
		charge := summary.Charges[idx]
		occurredAt := inLocation(charge.OccurredAt, location)
		merchant := strings.TrimSpace(charge.Merchant)
		if merchant == "" {
			merchant = "Unknown subscription"
		}

		line := fmt.Sprintf("- %s: $%.2f on %s", merchant, charge.Amount, occurredAt.Format("Jan 2"))

		accountLabel := strings.TrimSpace(charge.AccountLabel)
		if accountLabel != "" {
			line += fmt.Sprintf(" (%s)", accountLabel)
		}

		b.WriteString(line)
		b.WriteByte('\n')
	}

	if shown < len(summary.Charges) {
		fmt.Fprintf(b, "- ...and %d more\n", len(summary.Charges)-shown)
	}

	fmt.Fprintf(b, "\nTotal: $%.2f across %d unique subscriptions", summary.TotalAmount, len(summary.Charges))

	return strings.TrimSpace(b.String())
}

func inLocation(value time.Time, location *time.Location) time.Time {
	if location == nil {
		return value.Local()
	}

	return value.In(location)
}
