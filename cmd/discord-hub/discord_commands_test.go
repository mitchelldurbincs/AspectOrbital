package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"

	spokebridge "personal-infrastructure/cmd/discord-hub/spoke_bridge"
)

type fakeCommandRegistrar struct {
	existing []*discordgo.ApplicationCommand

	listErr   error
	editErr   error
	createErr error

	listCalls   int
	editCalls   int
	createCalls int

	lastEditedID string
	lastEdit     *discordgo.ApplicationCommand
	lastCreate   *discordgo.ApplicationCommand
}

func (f *fakeCommandRegistrar) ApplicationCommands(_ string, _ string, _ ...discordgo.RequestOption) ([]*discordgo.ApplicationCommand, error) {
	f.listCalls++

	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.existing, nil
}

func (f *fakeCommandRegistrar) ApplicationCommandEdit(_ string, _ string, cmdID string, cmd *discordgo.ApplicationCommand, _ ...discordgo.RequestOption) (*discordgo.ApplicationCommand, error) {
	f.editCalls++
	f.lastEditedID = cmdID
	f.lastEdit = cmd

	if f.editErr != nil {
		return nil, f.editErr
	}

	return &discordgo.ApplicationCommand{ID: cmdID, Name: cmd.Name, Description: cmd.Description}, nil
}

func (f *fakeCommandRegistrar) ApplicationCommandCreate(_ string, _ string, cmd *discordgo.ApplicationCommand, _ ...discordgo.RequestOption) (*discordgo.ApplicationCommand, error) {
	f.createCalls++
	f.lastCreate = cmd

	if f.createErr != nil {
		return nil, f.createErr
	}

	return &discordgo.ApplicationCommand{ID: "created", Name: cmd.Name, Description: cmd.Description}, nil
}

func TestUpsertCommandEditsExistingCommand(t *testing.T) {
	client := &fakeCommandRegistrar{
		existing: []*discordgo.ApplicationCommand{{ID: "123", Name: "ping", Description: "old"}},
	}

	cmd := &discordgo.ApplicationCommand{Name: "ping", Description: "new description"}
	updated, err := upsertCommand(client, "app", "guild", cmd)
	if err != nil {
		t.Fatalf("upsertCommand returned error: %v", err)
	}

	if client.editCalls != 1 || client.createCalls != 0 {
		t.Fatalf("expected edit=1 create=0, got edit=%d create=%d", client.editCalls, client.createCalls)
	}
	if client.lastEditedID != "123" {
		t.Fatalf("expected edit of command ID 123, got %q", client.lastEditedID)
	}
	if updated == nil || updated.ID != "123" {
		t.Fatalf("unexpected updated command: %#v", updated)
	}
}

func TestUpsertCommandCreatesWhenNotFound(t *testing.T) {
	client := &fakeCommandRegistrar{
		existing: []*discordgo.ApplicationCommand{{ID: "111", Name: "other"}},
	}

	cmd := &discordgo.ApplicationCommand{Name: "ping", Description: "created"}
	created, err := upsertCommand(client, "app", "guild", cmd)
	if err != nil {
		t.Fatalf("upsertCommand returned error: %v", err)
	}

	if client.editCalls != 0 || client.createCalls != 1 {
		t.Fatalf("expected edit=0 create=1, got edit=%d create=%d", client.editCalls, client.createCalls)
	}
	if created == nil || created.ID != "created" {
		t.Fatalf("unexpected created command: %#v", created)
	}
}

func TestUpsertCommandReturnsHelpfulListError(t *testing.T) {
	client := &fakeCommandRegistrar{listErr: errors.New("boom")}

	_, err := upsertCommand(client, "app", "guild", &discordgo.ApplicationCommand{Name: "ping"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "could not list existing commands for /ping") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpsertPingCommandCreatesPingSlashCommand(t *testing.T) {
	client := &fakeCommandRegistrar{}

	created, err := upsertPingCommand(client, "app", "guild")
	if err != nil {
		t.Fatalf("upsertPingCommand returned error: %v", err)
	}

	if created == nil {
		t.Fatal("expected created command, got nil")
	}
	if created.Name != pingCommandName {
		t.Fatalf("expected command name %q, got %q", pingCommandName, created.Name)
	}
	if created.Description == "" {
		t.Fatal("expected non-empty ping command description")
	}
	if client.createCalls != 1 {
		t.Fatalf("expected one create call, got %d", client.createCalls)
	}
}

func TestUpsertSpokeCommandsReusesListedCommands(t *testing.T) {
	client := &fakeCommandRegistrar{
		existing: []*discordgo.ApplicationCommand{{ID: "status-1", Name: "status", Description: "old"}},
	}

	bridge, err := spokebridge.NewBridge(nil, nil, "", "http://127.0.0.1:8090/control/commands", "http://127.0.0.1:8090/control/command", map[string]spokebridge.CommandSpec{
		"status": {Name: "status", Description: "new status"},
		"resume": {Name: "resume", Description: "resume"},
	})
	if err != nil {
		t.Fatalf("NewBridge returned error: %v", err)
	}

	err = upsertSpokeCommands(client, "app", "guild", bridge)
	if err != nil {
		t.Fatalf("upsertSpokeCommands returned error: %v", err)
	}

	if client.listCalls != 1 {
		t.Fatalf("expected one list call for spoke sync, got %d", client.listCalls)
	}
	if client.editCalls != 1 {
		t.Fatalf("expected one edit call for existing status command, got %d", client.editCalls)
	}
	if client.createCalls != 1 {
		t.Fatalf("expected one create call for new resume command, got %d", client.createCalls)
	}
}

func TestUpsertSpokeCommandsReturnsListError(t *testing.T) {
	client := &fakeCommandRegistrar{listErr: errors.New("listing failed")}

	bridge, err := spokebridge.NewBridge(nil, nil, "", "http://127.0.0.1:8090/control/commands", "http://127.0.0.1:8090/control/command", map[string]spokebridge.CommandSpec{"status": {Name: "status"}})
	if err != nil {
		t.Fatalf("NewBridge returned error: %v", err)
	}
	err = upsertSpokeCommands(client, "app", "guild", bridge)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "could not list existing commands for spoke sync") {
		t.Fatalf("unexpected error: %v", err)
	}
}
