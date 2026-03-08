package accountability

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"personal-infrastructure/pkg/beeminder"
)

type fakeBeeminder struct{ calls []beeminder.DatapointRequest }

func (f *fakeBeeminder) CreateDatapoint(_ context.Context, req beeminder.DatapointRequest) error {
	f.calls = append(f.calls, req)
	return nil
}

func testDBPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "accountability.sqlite")
	if err := Bootstrap(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRestartRecoveryKeepsPendingCommitments(t *testing.T) {
	path := testDBPath(t)
	bee := &fakeBeeminder{}
	now := time.Now().UTC()
	svc := NewService(path, bee, time.Minute)
	svc.now = func() time.Time { return now }

	_, err := svc.Commit(context.Background(), "u1", "write", "goal", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	restarted := NewService(path, bee, time.Minute)
	got, err := restarted.StatusForUser(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusPending {
		t.Fatalf("expected pending after restart, got %s", got.Status)
	}
}

func TestDeadlineTransitionMarksOverdueAndWritesConsequence(t *testing.T) {
	path := testDBPath(t)
	bee := &fakeBeeminder{}
	now := time.Now().UTC()
	svc := NewService(path, bee, time.Minute)
	svc.now = func() time.Time { return now }
	_, err := svc.Commit(context.Background(), "u1", "write", "goal", now.Add(5*time.Minute))
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
	if len(bee.calls) != 1 || bee.calls[0].Value != -1 {
		t.Fatalf("expected failure datapoint, got %#v", bee.calls)
	}
}

func TestProofHandlingIsIdempotent(t *testing.T) {
	path := testDBPath(t)
	bee := &fakeBeeminder{}
	now := time.Now().UTC()
	svc := NewService(path, bee, time.Minute)
	svc.now = func() time.Time { return now }
	_, err := svc.Commit(context.Background(), "u1", "write", "goal", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	meta := AttachmentMetadata{ID: "att1", Filename: "proof.png", URL: "https://example"}
	first, err := svc.SubmitProof(context.Background(), "u1", meta)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != StatusSuccess {
		t.Fatalf("expected success, got %s", first.Status)
	}
	if len(bee.calls) != 1 || bee.calls[0].Value != 1 {
		t.Fatalf("expected one success datapoint, got %#v", bee.calls)
	}

	if _, err := svc.SubmitProof(context.Background(), "u1", meta); err == nil {
		t.Fatal("expected second proof submit to fail without active commitment")
	}
	if len(bee.calls) != 1 {
		t.Fatalf("expected beeminder calls to remain idempotent, got %#v", bee.calls)
	}
}

func TestBootstrapCreatesDBFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	if err := Bootstrap(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected sqlite db file to exist: %v", err)
	}
}
