package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func TestResolveApplicationIDReturnsStateUserID(t *testing.T) {
	session := &discordgo.Session{
		State: &discordgo.State{
			Ready: discordgo.Ready{
				User: &discordgo.User{ID: "app-123"},
			},
		},
	}

	id, err := resolveApplicationID(session)
	if err != nil {
		t.Fatalf("resolveApplicationID returned error: %v", err)
	}
	if id != "app-123" {
		t.Fatalf("expected app-123, got %q", id)
	}
}

func TestResolveApplicationIDReturnsErrorWhenUnavailable(t *testing.T) {
	_, err := resolveApplicationID(&discordgo.Session{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestNewHTTPServerRegistersHealthAndNotifyHandlers(t *testing.T) {
	handler := testHubHandler(&fakeMessageSender{})
	server := newHTTPServer("127.0.0.1:0", handler)
	if server.ReadTimeout != 10*time.Second {
		t.Fatalf("expected ReadTimeout=10s, got %v", server.ReadTimeout)
	}
	if server.WriteTimeout != 15*time.Second {
		t.Fatalf("expected WriteTimeout=15s, got %v", server.WriteTimeout)
	}
	if server.IdleTimeout != 60*time.Second {
		t.Fatalf("expected IdleTimeout=60s, got %v", server.IdleTimeout)
	}
	if server.MaxHeaderBytes != 1<<20 {
		t.Fatalf("expected MaxHeaderBytes=%d, got %d", 1<<20, server.MaxHeaderBytes)
	}

	healthReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthRec := httptest.NewRecorder()
	server.Handler.ServeHTTP(healthRec, healthReq)
	if healthRec.Code != http.StatusOK {
		t.Fatalf("expected /healthz status %d, got %d", http.StatusOK, healthRec.Code)
	}

	notifyReq := httptest.NewRequest(http.MethodGet, "/notify", nil)
	notifyRec := httptest.NewRecorder()
	server.Handler.ServeHTTP(notifyRec, notifyReq)
	if notifyRec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected /notify status %d for GET, got %d", http.StatusMethodNotAllowed, notifyRec.Code)
	}
}
