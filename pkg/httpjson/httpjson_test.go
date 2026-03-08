package httpjson

import (
	"net/http/httptest"
	"strings"
	"testing"
)

type samplePayload struct {
	Name string `json:"name"`
}

func TestDecodeStrictJSONBodyRejectsUnknownField(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"ok","extra":1}`))
	var out samplePayload

	err := DecodeStrictJSONBody(req, &out, 1<<20)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if got, want := err.Error(), `invalid JSON payload: unknown field "extra"`; got != want {
		t.Fatalf("unexpected error: got %q want %q", got, want)
	}
}

func TestDecodeStrictJSONBodyRejectsMultipleObjects(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"ok"}{"name":"second"}`))
	var out samplePayload

	err := DecodeStrictJSONBody(req, &out, 1<<20)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if got, want := err.Error(), "request body must contain a single JSON object"; got != want {
		t.Fatalf("unexpected error: got %q want %q", got, want)
	}
}

func TestDecodeStrictJSONBodyRejectsMalformedPayload(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":`))
	var out samplePayload

	err := DecodeStrictJSONBody(req, &out, 1<<20)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if got, want := err.Error(), "invalid JSON payload"; got != want {
		t.Fatalf("unexpected error: got %q want %q", got, want)
	}
}
