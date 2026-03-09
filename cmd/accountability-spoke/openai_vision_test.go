package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEvaluateImageRequiresConfiguredClient(t *testing.T) {
	var client *openAIVisionClient

	_, err := client.EvaluateImage(context.Background(), "https://example.com/image.jpg", "inside a car")
	if err == nil {
		t.Fatal("expected error when client is nil")
	}
	if !strings.Contains(err.Error(), "openai client is not configured") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEvaluateImageReturnsOpenAIStatusBodyOnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad prompt"))
	}))
	defer server.Close()

	client := newOpenAIVisionClient(server.URL, "test-key", "gpt-4.1-mini", server.Client())
	_, err := client.EvaluateImage(context.Background(), "https://example.com/image.jpg", "inside a car")
	if err == nil {
		t.Fatal("expected error for non-2xx response")
	}
	if !strings.Contains(err.Error(), "openai API error (400 Bad Request): bad prompt") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateEvaluateImageInput(t *testing.T) {
	imageURL, prompt, err := validateEvaluateImageInput("  https://example.com/x.png  ", "  photo of desk  ")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if imageURL != "https://example.com/x.png" {
		t.Fatalf("unexpected image URL: %q", imageURL)
	}
	if prompt != "photo of desk" {
		t.Fatalf("unexpected prompt: %q", prompt)
	}

	_, _, err = validateEvaluateImageInput("", "prompt")
	if err == nil || !strings.Contains(err.Error(), "image URL is required") {
		t.Fatalf("unexpected missing image URL error: %v", err)
	}

	_, _, err = validateEvaluateImageInput("https://example.com/x.png", "")
	if err == nil || !strings.Contains(err.Error(), "prompt is required") {
		t.Fatalf("unexpected missing prompt error: %v", err)
	}
}

func TestParseCompletionContent(t *testing.T) {
	content, err := parseCompletionContent([]byte(`{"choices":[{"message":{"content":" {\"match\":true,\"confidence\":0.9,\"reason\":\"ok\"} "}}]}`))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if content != `{"match":true,"confidence":0.9,"reason":"ok"}` {
		t.Fatalf("unexpected content: %q", content)
	}

	_, err = parseCompletionContent([]byte(`{"choices":[]}`))
	if err == nil || !strings.Contains(err.Error(), "openai response contained no choices") {
		t.Fatalf("unexpected no choices error: %v", err)
	}

	_, err = parseCompletionContent([]byte(`{"choices":[{"message":{"content":"   "}}]}`))
	if err == nil || !strings.Contains(err.Error(), "openai response content was empty") {
		t.Fatalf("unexpected empty content error: %v", err)
	}
}

func TestParseVisionEvaluation(t *testing.T) {
	evaluation, err := parseVisionEvaluation(`{"match":true,"confidence":1.8,"reason":"  looks good  "}`)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !evaluation.Match {
		t.Fatal("expected match to be true")
	}
	if evaluation.Confidence != 1 {
		t.Fatalf("expected clamped confidence 1, got: %v", evaluation.Confidence)
	}
	if evaluation.Reason != "looks good" {
		t.Fatalf("unexpected reason: %q", evaluation.Reason)
	}

	evaluation, err = parseVisionEvaluation(`{"match":false,"confidence":-0.1,"reason":"no"}`)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if evaluation.Confidence != 0 {
		t.Fatalf("expected clamped confidence 0, got: %v", evaluation.Confidence)
	}

	_, err = parseVisionEvaluation("not-json")
	if err == nil || !strings.Contains(err.Error(), "failed to parse openai classifier JSON") {
		t.Fatalf("unexpected malformed JSON error: %v", err)
	}
}
