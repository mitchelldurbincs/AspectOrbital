package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"personal-infrastructure/pkg/accountability"
)

type stubVisionEvaluator struct {
	calls      int
	match      bool
	confidence float64
	reason     string
	err        error
}

func (s *stubVisionEvaluator) EvaluateImage(context.Context, string, string) (visionEvaluation, error) {
	s.calls++
	if s.err != nil {
		return visionEvaluation{}, s.err
	}
	return visionEvaluation{Match: s.match, Confidence: s.confidence, Reason: s.reason}, nil
}

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

func TestPolicyEvaluateManualAttachmentRequiresRealAttachment(t *testing.T) {
	catalog := policyCatalog{}

	evaluation, err := catalog.Evaluate(context.Background(), accountability.Commitment{PolicyEngine: "manual_attachment", PolicyConfig: `{}`}, accountability.AttachmentMetadata{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.Pass {
		t.Fatal("expected empty attachment to fail")
	}

	evaluation, err = catalog.Evaluate(context.Background(), accountability.Commitment{PolicyEngine: "manual_attachment", PolicyConfig: `{}`}, accountability.AttachmentMetadata{ID: "a1"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.Pass {
		t.Fatal("expected attachment with only id to fail")
	}

	evaluation, err = catalog.Evaluate(context.Background(), accountability.Commitment{PolicyEngine: "manual_attachment", PolicyConfig: `{}`}, accountability.AttachmentMetadata{ID: "a1", Filename: "proof.png"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !evaluation.Pass {
		t.Fatalf("expected attachment with filename to pass, got %#v", evaluation)
	}

	evaluation, err = catalog.Evaluate(context.Background(), accountability.Commitment{PolicyEngine: "manual_attachment", PolicyConfig: `{}`}, accountability.AttachmentMetadata{ID: "a2", URL: "https://cdn.discordapp.com/proof.png"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !evaluation.Pass {
		t.Fatalf("expected attachment with url to pass, got %#v", evaluation)
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

func TestPolicyEvaluateOpenAIVisionRejectsInvalidAttachmentURL(t *testing.T) {
	vision := &stubVisionEvaluator{match: true, confidence: 1}
	catalog := policyCatalog{vision: vision}

	evaluation, err := catalog.Evaluate(
		context.Background(),
		accountability.Commitment{PolicyEngine: "openai_vision", PolicyConfig: `{"prompt":"inside a car","minConfidence":0.8}`},
		accountability.AttachmentMetadata{URL: "data:image/png;base64,AAAA", ContentType: "image/png"},
		"",
	)
	if err != nil {
		t.Fatalf("expected no internal error, got: %v", err)
	}
	if evaluation.Pass {
		t.Fatal("expected invalid attachment URL to fail policy")
	}
	if !strings.Contains(strings.ToLower(evaluation.Reason), "invalid proof attachment url") {
		t.Fatalf("unexpected failure reason: %q", evaluation.Reason)
	}
	if vision.calls != 0 {
		t.Fatalf("expected no vision calls for invalid URL, got %d", vision.calls)
	}
}

func TestPolicyEvaluateOpenAIVisionRejectsNonImageContentType(t *testing.T) {
	vision := &stubVisionEvaluator{match: true, confidence: 1}
	catalog := policyCatalog{vision: vision}

	evaluation, err := catalog.Evaluate(
		context.Background(),
		accountability.Commitment{PolicyEngine: "openai_vision", PolicyConfig: `{"prompt":"inside a car","minConfidence":0.8}`},
		accountability.AttachmentMetadata{URL: "https://cdn.discordapp.com/proof.txt", ContentType: "text/plain"},
		"",
	)
	if err != nil {
		t.Fatalf("expected no internal error, got: %v", err)
	}
	if evaluation.Pass {
		t.Fatal("expected non-image content type to fail policy")
	}
	if !strings.Contains(evaluation.Reason, "content type") {
		t.Fatalf("unexpected failure reason: %q", evaluation.Reason)
	}
	if vision.calls != 0 {
		t.Fatalf("expected no vision calls for invalid content type, got %d", vision.calls)
	}
}

func TestPolicyEvaluateOpenAIVisionCallsVisionForValidAttachment(t *testing.T) {
	vision := &stubVisionEvaluator{match: true, confidence: 0.91, reason: "looks good"}
	catalog := policyCatalog{vision: vision}

	evaluation, err := catalog.Evaluate(
		context.Background(),
		accountability.Commitment{PolicyEngine: "openai_vision", PolicyConfig: `{"prompt":"inside a car","minConfidence":0.8}`},
		accountability.AttachmentMetadata{URL: "https://cdn.discordapp.com/attachments/1/2/proof.png", ContentType: "image/png"},
		"",
	)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !evaluation.Pass {
		t.Fatalf("expected policy to pass, got: %#v", evaluation)
	}
	if vision.calls != 1 {
		t.Fatalf("expected one vision call, got %d", vision.calls)
	}
}

func TestPolicyEvaluateOpenAIVisionPropagatesVisionError(t *testing.T) {
	vision := &stubVisionEvaluator{err: fmt.Errorf("boom")}
	catalog := policyCatalog{vision: vision}

	_, err := catalog.Evaluate(
		context.Background(),
		accountability.Commitment{PolicyEngine: "openai_vision", PolicyConfig: `{"prompt":"inside a car","minConfidence":0.8}`},
		accountability.AttachmentMetadata{URL: "https://cdn.discordapp.com/attachments/1/2/proof.png", ContentType: "image/png"},
		"",
	)
	if err == nil || !strings.Contains(err.Error(), "openai vision check failed") {
		t.Fatalf("unexpected vision error: %v", err)
	}
}
