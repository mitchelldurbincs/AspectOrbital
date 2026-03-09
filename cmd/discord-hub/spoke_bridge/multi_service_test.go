package spokebridge

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

func TestFetchAllCommandsWithRetryMergesCatalogs(t *testing.T) {
	alpha := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"version":1,"service":"alpha","commands":[{"name":"status","description":"status"}]}`)
	}))
	defer alpha.Close()

	bravo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"version":1,"service":"bravo","commands":[{"name":"sync","description":"sync"}]}`)
	}))
	defer bravo.Close()

	bridge := NewBridgeWithServices(
		log.New(io.Discard, "", 0),
		alpha.Client(),
		[]ServiceDefinition{
			{Name: "alpha", CommandsURL: alpha.URL, ExecuteURL: alpha.URL},
			{Name: "bravo", CommandsURL: bravo.URL, ExecuteURL: bravo.URL},
		},
		nil,
		nil,
	)

	commands, owners, counts, err := bridge.fetchAllCommandsWithRetry()
	if err != nil {
		t.Fatalf("fetchAllCommandsWithRetry() error = %v", err)
	}

	if len(commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(commands))
	}
	if owners["status"] != "alpha" || owners["sync"] != "bravo" {
		t.Fatalf("unexpected owners: %#v", owners)
	}
	if counts["alpha"] != 1 || counts["bravo"] != 1 {
		t.Fatalf("unexpected per-service counts: %#v", counts)
	}
}

func TestFetchAllCommandsWithRetryRejectsDuplicateCommands(t *testing.T) {
	alpha := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"version":1,"service":"alpha","commands":[{"name":"status","description":"status"}]}`)
	}))
	defer alpha.Close()

	bravo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"version":1,"service":"bravo","commands":[{"name":"status","description":"status"}]}`)
	}))
	defer bravo.Close()

	bridge := NewBridgeWithServices(
		log.New(io.Discard, "", 0),
		alpha.Client(),
		[]ServiceDefinition{
			{Name: "alpha", CommandsURL: alpha.URL, ExecuteURL: alpha.URL},
			{Name: "bravo", CommandsURL: bravo.URL, ExecuteURL: bravo.URL},
		},
		nil,
		nil,
	)

	_, _, _, err := bridge.fetchAllCommandsWithRetry()
	if err == nil {
		t.Fatal("expected duplicate command error, got nil")
	}
	if !strings.Contains(err.Error(), `duplicate command "status" provided by services "alpha" and "bravo"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFetchAllCommandsWithRetryContinuesWhenOneServiceUnavailable(t *testing.T) {
	alpha := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"version":1,"service":"alpha","commands":[{"name":"status","description":"status"}]}`)
	}))
	defer alpha.Close()

	bridge := NewBridgeWithServices(
		log.New(io.Discard, "", 0),
		alpha.Client(),
		[]ServiceDefinition{
			{Name: "alpha", CommandsURL: alpha.URL, ExecuteURL: alpha.URL},
			{Name: "bravo", CommandsURL: "http://127.0.0.1:1", ExecuteURL: "http://127.0.0.1:1"},
		},
		nil,
		nil,
	)

	commands, owners, counts, err := bridge.fetchAllCommandsWithRetry()
	if err != nil {
		t.Fatalf("fetchAllCommandsWithRetry() error = %v", err)
	}

	if len(commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(commands))
	}
	if owners["status"] != "alpha" {
		t.Fatalf("unexpected owners: %#v", owners)
	}
	if counts["alpha"] != 1 {
		t.Fatalf("unexpected per-service counts: %#v", counts)
	}
	if _, ok := counts["bravo"]; ok {
		t.Fatalf("expected unavailable service to be omitted from counts: %#v", counts)
	}
}

func TestFetchAllCommandsWithRetryReturnsErrorWhenNoServicesReachable(t *testing.T) {
	bridge := NewBridgeWithServices(
		log.New(io.Discard, "", 0),
		http.DefaultClient,
		[]ServiceDefinition{{Name: "alpha", CommandsURL: "http://127.0.0.1:1", ExecuteURL: "http://127.0.0.1:1"}},
		nil,
		nil,
	)

	_, _, _, err := bridge.fetchAllCommandsWithRetry()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no spoke command catalogs are currently reachable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteCommandRoutesToOwningService(t *testing.T) {
	var alphaCalled, bravoCalled bool
	alpha := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		alphaCalled = true
		_, _ = io.WriteString(w, `{"status":"ok","command":"status","message":"from-alpha"}`)
	}))
	defer alpha.Close()

	bravo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bravoCalled = true
		var req commandRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Command != "sync" {
			t.Fatalf("expected sync command, got %q", req.Command)
		}
		_, _ = io.WriteString(w, `{"status":"ok","command":"sync","message":"from-bravo"}`)
	}))
	defer bravo.Close()

	bridge := NewBridgeWithServices(
		log.New(io.Discard, "", 0),
		alpha.Client(),
		[]ServiceDefinition{
			{Name: "alpha", CommandsURL: alpha.URL, ExecuteURL: alpha.URL},
			{Name: "bravo", CommandsURL: bravo.URL, ExecuteURL: bravo.URL},
		},
		map[string]CommandSpec{"sync": {Name: "sync"}},
		map[string]string{"sync": "bravo"},
	)

	msg, err := bridge.ExecuteCommand(context.Background(), "sync", spokecontract.CommandContext{DiscordUserID: "u-1"}, nil)
	if err != nil {
		t.Fatalf("ExecuteCommand() error = %v", err)
	}
	if msg != "from-bravo" {
		t.Fatalf("unexpected message: %q", msg)
	}
	if alphaCalled {
		t.Fatal("alpha endpoint should not be called")
	}
	if !bravoCalled {
		t.Fatal("bravo endpoint should be called")
	}
}
