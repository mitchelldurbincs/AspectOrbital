package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

type fakeCommandRegistrar struct {
	existing []*discordgo.ApplicationCommand

	listErr   error
	editErr   error
	createErr error

	editCalls   int
	createCalls int

	lastEditedID string
	lastEdit     *discordgo.ApplicationCommand
	lastCreate   *discordgo.ApplicationCommand
}

func (f *fakeCommandRegistrar) ApplicationCommands(_ string, _ string, _ ...discordgo.RequestOption) ([]*discordgo.ApplicationCommand, error) {
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
