package accountability

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "accountability.sqlite")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	if err := Bootstrap(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

func testService(t *testing.T, db *sql.DB, pollInterval, expiryGrace time.Duration) *Service {
	t.Helper()
	svc, err := NewService(db, pollInterval, expiryGrace)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestRestartRecoveryKeepsPendingCommitments(t *testing.T) {
	db := testDB(t)
	now := time.Now().UTC()
	svc := testService(t, db, time.Minute, 12*time.Hour)
	svc.now = func() time.Time { return now }

	_, err := svc.Commit(context.Background(), "u1", "write", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	restarted := testService(t, db, time.Minute, 12*time.Hour)
	got, err := restarted.StatusForUser(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusPending {
		t.Fatalf("expected pending after restart, got %s", got.Status)
	}
}

func TestDeadlineTransitionMarksOverdue(t *testing.T) {
	db := testDB(t)
	now := time.Now().UTC()
	svc := testService(t, db, time.Minute, 0)
	svc.now = func() time.Time { return now }
	_, err := svc.Commit(context.Background(), "u1", "write", now.Add(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	svc.now = func() time.Time { return now.Add(10 * time.Minute) }
	n, err := svc.ExpireOverdue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 expired commitment, got %d", n)
	}
	got, err := svc.StatusForUser(context.Background(), "u1")
	if err == nil {
		t.Fatalf("expected no active commitment after expiry, got %#v", got)
	}
}

func TestProofHandlingIsIdempotent(t *testing.T) {
	db := testDB(t)
	now := time.Now().UTC()
	svc := testService(t, db, time.Minute, 12*time.Hour)
	svc.now = func() time.Time { return now }
	_, err := svc.Commit(context.Background(), "u1", "write", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	meta := AttachmentMetadata{ID: "att1", Filename: "proof.png", URL: "https://example"}
	first, err := svc.SubmitProof(context.Background(), "u1", ProofSubmission{Attachment: meta, Text: "done"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != StatusSuccess {
		t.Fatalf("expected success, got %s", first.Status)
	}

	if _, err := svc.SubmitProof(context.Background(), "u1", ProofSubmission{Attachment: meta, Text: "done"}); err == nil {
		t.Fatal("expected second proof submit to fail without active commitment")
	}
}

func TestLateProofMarksCommitmentFailedWithoutExpirySweep(t *testing.T) {
	db := testDB(t)
	now := time.Now().UTC()
	svc := testService(t, db, time.Minute, 12*time.Hour)
	svc.now = func() time.Time { return now }

	_, err := svc.Commit(context.Background(), "u1", "write", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	svc.now = func() time.Time { return now.Add(2 * time.Minute) }
	result, err := svc.SubmitProof(context.Background(), "u1", ProofSubmission{Text: "done", Verdict: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusFailed {
		t.Fatalf("expected failed status for late proof, got %s", result.Status)
	}
	if result.ProofMetadata == "" {
		t.Fatal("expected proof metadata to be stored for late proof")
	}
	if _, err := svc.StatusForUser(context.Background(), "u1"); err == nil {
		t.Fatal("expected no active commitment after late proof failure")
	}
}

func TestSnoozeHidesReminderUntilElapsed(t *testing.T) {
	db := testDB(t)
	now := time.Now().UTC()
	svc := testService(t, db, time.Minute, 12*time.Hour)
	svc.now = func() time.Time { return now }

	_, err := svc.Commit(context.Background(), "u1", "gym", now.Add(-10*time.Minute))
	if err == nil {
		t.Fatal("expected commit with past deadline to fail")
	}

	_, err = svc.Commit(context.Background(), "u1", "gym", now.Add(1*time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	svc.now = func() time.Time { return now.Add(2 * time.Minute) }
	commitment, err := svc.Snooze(context.Background(), "u1", 10*time.Minute, 60*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if commitment.SnoozedUntil.IsZero() {
		t.Fatal("expected snoozed_until to be populated")
	}

	reminders, err := svc.OverdueNeedingReminder(context.Background(), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(reminders) != 0 {
		t.Fatalf("expected no reminders while snoozed, got %d", len(reminders))
	}

	svc.now = func() time.Time { return now.Add(13 * time.Minute) }
	reminders, err = svc.OverdueNeedingReminder(context.Background(), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(reminders) != 1 {
		t.Fatalf("expected one reminder after snooze elapsed, got %d", len(reminders))
	}
}

func TestMarkReminderSentThrottlesReminders(t *testing.T) {
	db := testDB(t)
	now := time.Now().UTC()
	svc := testService(t, db, time.Minute, 12*time.Hour)
	svc.now = func() time.Time { return now }

	_, err := svc.Commit(context.Background(), "u1", "gym", now.Add(1*time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	svc.now = func() time.Time { return now.Add(3 * time.Minute) }
	reminders, err := svc.OverdueNeedingReminder(context.Background(), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(reminders) != 1 {
		t.Fatalf("expected one reminder, got %d", len(reminders))
	}

	if err := svc.MarkReminderSent(context.Background(), reminders[0].ID); err != nil {
		t.Fatal(err)
	}

	reminders, err = svc.OverdueNeedingReminder(context.Background(), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(reminders) != 0 {
		t.Fatalf("expected reminder throttling to suppress sends, got %d", len(reminders))
	}
}

func TestExpireOverdueUsesGracePeriod(t *testing.T) {
	db := testDB(t)
	now := time.Now().UTC()
	svc := testService(t, db, time.Minute, 30*time.Minute)
	svc.now = func() time.Time { return now }

	_, err := svc.Commit(context.Background(), "u1", "gym", now.Add(1*time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	svc.now = func() time.Time { return now.Add(5 * time.Minute) }
	n, err := svc.ExpireOverdue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected zero expirations before grace cutoff, got %d", n)
	}

	svc.now = func() time.Time { return now.Add(40 * time.Minute) }
	n, err = svc.ExpireOverdue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected one expiration after grace cutoff, got %d", n)
	}
}

func TestBootstrapCreatesDBFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := Bootstrap(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected sqlite db file to exist: %v", err)
	}
}
