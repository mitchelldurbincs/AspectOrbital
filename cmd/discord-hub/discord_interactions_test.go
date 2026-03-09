package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"

	spokebridge "personal-infrastructure/cmd/discord-hub/spoke_bridge"
	"personal-infrastructure/pkg/spokecontract"
)

func commandInteraction(name string, options []*discordgo.ApplicationCommandInteractionDataOption) *discordgo.InteractionCreate {
	return &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionApplicationCommand,
			User: &discordgo.User{ID: "test-user"},
			Data: discordgo.ApplicationCommandInteractionData{
				Name:    name,
				Options: options,
			},
		},
	}
}

func TestInteractionOptionValuesNormalizesNamesAndValues(t *testing.T) {
	options := []*discordgo.ApplicationCommandInteractionDataOption{
		{Name: " Arg ", Value: "  hello "},
		{Name: "Flag", Value: true},
		{Name: "Count", Value: 7},
		{Name: "Ratio", Value: 1.25},
		{Name: "Misc", Value: []int{1, 2}},
		{Name: "   ", Value: "ignored"},
	}

	got := interactionOptionValues(options)
	want := map[string]any{
		"arg":   "hello",
		"flag":  true,
		"count": 7,
		"ratio": 1.25,
		"misc":  "[1 2]",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected options\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestInteractionOptionValuesReturnsNilWhenNoUsableOptions(t *testing.T) {
	got := interactionOptionValues([]*discordgo.ApplicationCommandInteractionDataOption{{Name: "   "}})
	if got != nil {
		t.Fatalf("expected nil options map, got %#v", got)
	}
}

func TestInteractionOptionValuesSupportsAttachmentMetadata(t *testing.T) {
	options := []*discordgo.ApplicationCommandInteractionDataOption{
		{Name: "proof", Value: &discordgo.MessageAttachment{ID: "a1", Filename: "proof.png", URL: "https://cdn/proof.png", ContentType: "image/png", Size: 42}},
	}

	got := interactionOptionValues(options)
	proof, ok := got["proof"].(map[string]any)
	if !ok {
		t.Fatalf("expected proof map, got %#v", got["proof"])
	}
	if proof["id"] != "a1" || proof["filename"] != "proof.png" {
		t.Fatalf("unexpected proof metadata: %#v", proof)
	}
}

func TestInteractionHandlerRespondsToPing(t *testing.T) {
	var messages []string

	prev := respondEphemeralFunc
	respondEphemeralFunc = func(_ *discordgo.Session, _ *discordgo.Interaction, message string) error {
		messages = append(messages, message)
		return nil
	}
	t.Cleanup(func() {
		respondEphemeralFunc = prev
	})

	handler := interactionHandler(log.New(io.Discard, "", 0), nil)
	handler(nil, commandInteraction(pingCommandName, nil))

	if len(messages) != 1 || messages[0] != "pong" {
		t.Fatalf("unexpected messages: %#v", messages)
	}
}

func TestInteractionHandlerForwardsSpokeCommands(t *testing.T) {
	var captured spokecontract.CommandRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","command":"status","message":"from spoke"}`))
	}))
	defer server.Close()

	bridge := spokebridge.NewBridge(log.New(io.Discard, "", 0), server.Client(), server.URL, server.URL, map[string]spokebridge.CommandSpec{
		"status": {Name: "status"},
	})

	var messages []string
	prevDefer := deferEphemeralFunc
	deferEphemeralFunc = func(_ *discordgo.Session, _ *discordgo.Interaction) error {
		return nil
	}
	prevFollowup := followupEphemeralFunc
	followupEphemeralFunc = func(_ *discordgo.Session, _ *discordgo.Interaction, message string) error {
		messages = append(messages, message)
		return nil
	}
	t.Cleanup(func() {
		deferEphemeralFunc = prevDefer
		followupEphemeralFunc = prevFollowup
	})

	handler := interactionHandler(log.New(io.Discard, "", 0), bridge)
	interaction := commandInteraction("status", []*discordgo.ApplicationCommandInteractionDataOption{{Name: "duration", Value: " 30m "}})
	interaction.Interaction.Member = &discordgo.Member{User: &discordgo.User{ID: "u-123"}}
	interaction.Interaction.GuildID = "g-456"
	interaction.Interaction.ChannelID = "c-789"
	handler(nil, interaction)

	if len(messages) != 1 || messages[0] != "from spoke" {
		t.Fatalf("unexpected response messages: %#v", messages)
	}
	if captured.Command != "status" {
		t.Fatalf("unexpected command forwarded to spoke: %q", captured.Command)
	}
	if captured.Context != (spokecontract.CommandContext{DiscordUserID: "u-123", GuildID: "g-456", ChannelID: "c-789"}) {
		t.Fatalf("unexpected forwarded context: %#v", captured.Context)
	}
	if got := strings.TrimSpace(captured.Options["duration"].(string)); got != "30m" {
		t.Fatalf("expected forwarded duration 30m, got %#v", captured.Options["duration"])
	}
}

func TestInteractionHandlerRespondsWhenCommandUnavailable(t *testing.T) {
	var messages []string

	prev := respondEphemeralFunc
	respondEphemeralFunc = func(_ *discordgo.Session, _ *discordgo.Interaction, message string) error {
		messages = append(messages, message)
		return nil
	}
	t.Cleanup(func() {
		respondEphemeralFunc = prev
	})

	handler := interactionHandler(log.New(io.Discard, "", 0), nil)
	handler(nil, commandInteraction("status", nil))

	if len(messages) != 1 {
		t.Fatalf("expected exactly one response, got %d", len(messages))
	}
	if messages[0] != "That command is not available right now. Try again in a moment." {
		t.Fatalf("unexpected unavailable-command message: %q", messages[0])
	}
}

func TestInteractionHandlerFormatsSpokeCommandFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "execution failed", http.StatusBadGateway)
	}))
	defer server.Close()

	bridge := spokebridge.NewBridge(log.New(io.Discard, "", 0), server.Client(), server.URL, server.URL, map[string]spokebridge.CommandSpec{
		"status": {Name: "status"},
	})

	var messages []string
	prevDefer := deferEphemeralFunc
	deferEphemeralFunc = func(_ *discordgo.Session, _ *discordgo.Interaction) error {
		return nil
	}
	prevFollowup := followupEphemeralFunc
	followupEphemeralFunc = func(_ *discordgo.Session, _ *discordgo.Interaction, message string) error {
		messages = append(messages, message)
		return nil
	}
	t.Cleanup(func() {
		deferEphemeralFunc = prevDefer
		followupEphemeralFunc = prevFollowup
	})

	handler := interactionHandler(log.New(io.Discard, "", 0), bridge)
	handler(nil, commandInteraction("status", nil))

	if len(messages) != 1 {
		t.Fatalf("expected one response message, got %#v", messages)
	}
	if messages[0] != "Command failed: execution failed" {
		t.Fatalf("unexpected failure message: %q", messages[0])
	}
}
