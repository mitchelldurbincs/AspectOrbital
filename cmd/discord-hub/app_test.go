package main

import (
	"testing"

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
