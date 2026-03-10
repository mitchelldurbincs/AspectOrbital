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

	spokebridge "personal-infrastructure/cmd/discord-hub/spoke_bridge"
	"personal-infrastructure/pkg/spokecontract"
)

func TestFetchCommandsRejectsReservedPingCommand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method: %s", r.Method)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"version":1,"service":"test-spoke","commands":[{"name":"status","description":"Status command"},{"name":"ping","description":"reserved"}]}`)
	}))
	defer server.Close()

	bridge, err := spokebridge.NewBridge(log.New(io.Discard, "", 0), server.Client(), "", server.URL, server.URL, nil)
	if err != nil {
		t.Fatalf("NewBridge returned error: %v", err)
	}

	_, err = bridge.FetchCommands(context.Background())
	if err == nil {
		t.Fatal("expected reserved command error")
	}
	if !strings.Contains(err.Error(), `command "ping" is reserved`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFetchCommandsReturnsErrorForNon2xxResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "spoke unavailable", http.StatusBadGateway)
	}))
	defer server.Close()

	bridge, err := spokebridge.NewBridge(log.New(io.Discard, "", 0), server.Client(), "", server.URL, server.URL, nil)
	if err != nil {
		t.Fatalf("NewBridge returned error: %v", err)
	}

	_, err = bridge.FetchCommands(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "spoke command catalog request failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteCommandPostsRequestAndReturnsMessage(t *testing.T) {
	var captured spokecontract.CommandRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-command-token" {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok","command":"status","message":"done"}`)
	}))
	defer server.Close()

	bridge, err := spokebridge.NewBridge(log.New(io.Discard, "", 0), server.Client(), "test-command-token", server.URL, server.URL, map[string]spokebridge.CommandSpec{"status": {Name: "status", Description: "status"}})
	if err != nil {
		t.Fatalf("NewBridge returned error: %v", err)
	}

	message, err := bridge.ExecuteCommand(context.Background(), "status", spokecontract.CommandContext{DiscordUserID: "u-1"}, map[string]any{
		"duration": " 30m ",
		"flag":     true,
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
	if captured.Options["duration"] != " 30m " {
		t.Fatalf("expected exact duration option, got %#v", captured.Options["duration"])
	}
	if captured.Options["flag"] != true {
		t.Fatalf("expected flag option true, got %#v", captured.Options["flag"])
	}
}

func TestExecuteCommandRejectsInvalidOptionKey(t *testing.T) {
	bridge, err := spokebridge.NewBridge(log.New(io.Discard, "", 0), http.DefaultClient, "", "http://127.0.0.1:8090/control/commands", "http://127.0.0.1:8090/control/command", map[string]spokebridge.CommandSpec{"status": {Name: "status", Description: "status"}})
	if err != nil {
		t.Fatalf("NewBridge returned error: %v", err)
	}

	_, err = bridge.ExecuteCommand(context.Background(), "status", spokecontract.CommandContext{DiscordUserID: "u-1"}, map[string]any{"bad key": "ignored"})
	if err == nil {
		t.Fatal("expected invalid option key error")
	}
	if !strings.Contains(err.Error(), `option name "bad key" is invalid`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteCommandRejectsMissingMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok","command":"status","message":""}`)
	}))
	defer server.Close()

	bridge, err := spokebridge.NewBridge(log.New(io.Discard, "", 0), server.Client(), "", server.URL, server.URL, map[string]spokebridge.CommandSpec{"status": {Name: "status", Description: "status"}})
	if err != nil {
		t.Fatalf("NewBridge returned error: %v", err)
	}

	_, err = bridge.ExecuteCommand(context.Background(), "status", spokecontract.CommandContext{DiscordUserID: "u-1"}, nil)
	if err == nil {
		t.Fatal("expected error for missing response message")
	}
}

func TestExecuteCommandReturnsErrorMessageForNon2xxResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "execution failed", http.StatusInternalServerError)
	}))
	defer server.Close()

	bridge, err := spokebridge.NewBridge(log.New(io.Discard, "", 0), server.Client(), "", server.URL, server.URL, map[string]spokebridge.CommandSpec{"status": {Name: "status", Description: "status"}})
	if err != nil {
		t.Fatalf("NewBridge returned error: %v", err)
	}

	_, err = bridge.ExecuteCommand(context.Background(), "status", spokecontract.CommandContext{DiscordUserID: "u-1"}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "execution failed") {
		t.Fatalf("unexpected error message: %v", err)
	}
}
