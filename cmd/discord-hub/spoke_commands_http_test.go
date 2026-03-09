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

	"personal-infrastructure/pkg/spokecontract"
)

func TestFetchCommandsReturnsNormalizedCommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method: %s", r.Method)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"version":1,"service":"test-spoke","commands":[{"name":"status","description":"Status command"},{"name":"ping","description":"reserved"}]}`)
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
	if commands[0].Description != "Status command" {
		t.Fatalf("unexpected description: %q", commands[0].Description)
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

	message, err := bridge.ExecuteCommand(context.Background(), "status", spokecontract.CommandContext{DiscordUserID: "u-1"}, map[string]any{
		"duration": " 30m ",
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
	if captured.Context.DiscordUserID != "u-1" {
		t.Fatalf("expected forwarded context user id, got %#v", captured.Context)
	}
	if captured.Options["duration"] != "30m" {
		t.Fatalf("expected duration option 30m, got %#v", captured.Options["duration"])
	}
	if captured.Options["flag"] != true {
		t.Fatalf("expected flag option true, got %#v", captured.Options["flag"])
	}
	if _, exists := captured.Options["bad key"]; exists {
		t.Fatalf("did not expect invalid option key in request: %#v", captured.Options)
	}
}

func TestExecuteCommandRejectsMissingMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok","command":"status","message":""}`)
	}))
	defer server.Close()

	bridge := &spokeCommandBridge{
		httpClient: server.Client(),
		commandURL: server.URL,
	}

	_, err := bridge.ExecuteCommand(context.Background(), "status", spokecontract.CommandContext{DiscordUserID: "u-1"}, nil)
	if err == nil {
		t.Fatal("expected error for missing response message")
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

	_, err := bridge.ExecuteCommand(context.Background(), "status", spokecontract.CommandContext{DiscordUserID: "u-1"}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "execution failed") {
		t.Fatalf("unexpected error message: %v", err)
	}
}
