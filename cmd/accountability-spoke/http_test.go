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

func TestParseAttachmentSupportsContentTypeAliasesAndSize(t *testing.T) {
	attachment := parseAttachment(map[string]any{
		"id":           " a1 ",
		"filename":     " proof.png ",
		"url":          " https://cdn.discordapp.com/proof.png ",
		"content_type": " image/png ",
		"size":         42.0,
	})
	if attachment.ID != "a1" {
		t.Fatalf("unexpected id: %q", attachment.ID)
	}
	if attachment.Filename != "proof.png" {
		t.Fatalf("unexpected filename: %q", attachment.Filename)
	}
	if attachment.URL != "https://cdn.discordapp.com/proof.png" {
		t.Fatalf("unexpected URL: %q", attachment.URL)
	}
	if attachment.ContentType != "image/png" {
		t.Fatalf("unexpected content type: %q", attachment.ContentType)
	}
	if attachment.Size != 42 {
		t.Fatalf("unexpected size: %d", attachment.Size)
	}

	attachment = parseAttachment(map[string]any{"contentType": "image/jpeg"})
	if attachment.ContentType != "image/jpeg" {
		t.Fatalf("expected camelCase contentType fallback, got: %q", attachment.ContentType)
	}
}
