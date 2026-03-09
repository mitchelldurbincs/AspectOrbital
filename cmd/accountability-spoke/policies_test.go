package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"personal-infrastructure/pkg/accountability"
)

func TestLoadPolicyCatalogAndResolveDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policies.json")
	content := `{"version":1,"defaultPreset":"morning","presets":{"morning":{"task":"Gym check-in","engine":"text_reply","engineConfig":{"minChars":5}}}}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	catalog, err := loadPolicyCatalog(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	resolved, err := catalog.ResolveCommit("", "")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Preset != "morning" {
		t.Fatalf("expected default preset morning, got %q", resolved.Preset)
	}
	if resolved.Task != "Gym check-in" {
		t.Fatalf("expected default task from preset, got %q", resolved.Task)
	}
}

func TestPolicyEvaluateTextReply(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policies.json")
	content := `{"version":1,"defaultPreset":"reply","presets":{"reply":{"task":"Check in","engine":"text_reply","engineConfig":{"minChars":3}}}}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog, err := loadPolicyCatalog(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	evaluation, err := catalog.Evaluate(context.Background(), accountability.Commitment{PolicyEngine: "text_reply", PolicyConfig: `{"minChars":3}`}, accountability.AttachmentMetadata{}, "ok")
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.Pass {
		t.Fatal("expected short text to fail")
	}

	evaluation, err = catalog.Evaluate(context.Background(), accountability.Commitment{PolicyEngine: "text_reply", PolicyConfig: `{"minChars":3}`}, accountability.AttachmentMetadata{}, "done")
	if err != nil {
		t.Fatal(err)
	}
	if !evaluation.Pass {
		t.Fatal("expected long enough text to pass")
	}
}

func TestLoadPolicyCatalogFailsWhenOpenAIPresetWithoutClient(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policies.json")
	content := `{"version":1,"defaultPreset":"car","presets":{"car":{"task":"Car check","engine":"openai_vision","engineConfig":{"prompt":"inside a car","minConfidence":0.8}}}}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := loadPolicyCatalog(path, nil)
	if err == nil {
		t.Fatal("expected error when openai_vision preset is configured without OpenAI client")
	}
}
