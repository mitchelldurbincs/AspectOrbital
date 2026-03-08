package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchCommandsReturnsNormalizedCommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method: %s", r.Method)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"commands":[{"name":" STATUS ","description":""},{"name":"ping","description":"reserved"}]}`)
	}))
	defer server.Close()

	bridge := &spokeCommandBridge{
		log:         log.New(io.Discard, "", 0),
		httpClient:  server.Client(),
		commandsURL: server.URL,
	}

	commands, err := bridge.fetchCommands(context.Background())
	if err != nil {
		t.Fatalf("fetchCommands returned error: %v", err)
	}

	if len(commands) != 1 || commands[0].Name != "status" {
		t.Fatalf("unexpected normalized commands: %#v", commands)
	}
	if commands[0].Description != legacySpokeCommandDescription {
		t.Fatalf("expected fallback description %q, got %q", legacySpokeCommandDescription, commands[0].Description)
	}
}

func TestFetchCommandsReturnsErrorForNon2xxResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "spoke unavailable", http.StatusBadGateway)
	}))
	defer server.Close()

	bridge := &spokeCommandBridge{
		log:         log.New(io.Discard, "", 0),
		httpClient:  server.Client(),
		commandsURL: server.URL,
	}

	_, err := bridge.fetchCommands(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "spoke command catalog request failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteCommandPostsRequestAndReturnsMessage(t *testing.T) {
	var captured spokeCommandRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok","command":"status","message":"done"}`)
	}))
	defer server.Close()

	bridge := &spokeCommandBridge{
		httpClient: server.Client(),
		commandURL: server.URL,
	}

	message, err := bridge.ExecuteCommand(context.Background(), "status", map[string]any{
		"argument": " 30m ",
		"Flag":     true,
		"bad key":  "ignored",
	})
	if err != nil {
		t.Fatalf("ExecuteCommand returned error: %v", err)
	}
	if message != "done" {
		t.Fatalf("unexpected command message: %q", message)
	}

	if captured.Command != "status" {
		t.Fatalf("unexpected forwarded command: %q", captured.Command)
	}
	if captured.Argument != "30m" {
		t.Fatalf("expected forwarded argument 30m, got %q", captured.Argument)
	}
	if captured.Options["flag"] != true {
		t.Fatalf("expected flag option true, got %#v", captured.Options["flag"])
	}
	if _, exists := captured.Options["bad key"]; exists {
		t.Fatalf("did not expect invalid option key in request: %#v", captured.Options)
	}
}

func TestExecuteCommandUsesFallbackMessageWhenMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok","command":"status","message":""}`)
	}))
	defer server.Close()

	bridge := &spokeCommandBridge{
		httpClient: server.Client(),
		commandURL: server.URL,
	}

	message, err := bridge.ExecuteCommand(context.Background(), "status", nil)
	if err != nil {
		t.Fatalf("ExecuteCommand returned error: %v", err)
	}
	if message != "Command `status` acknowledged." {
		t.Fatalf("unexpected fallback message: %q", message)
	}
}

func TestExecuteCommandReturnsErrorMessageForNon2xxResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "execution failed", http.StatusInternalServerError)
	}))
	defer server.Close()

	bridge := &spokeCommandBridge{
		httpClient: server.Client(),
		commandURL: server.URL,
	}

	_, err := bridge.ExecuteCommand(context.Background(), "status", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "execution failed") {
		t.Fatalf("unexpected error message: %v", err)
	}
}
