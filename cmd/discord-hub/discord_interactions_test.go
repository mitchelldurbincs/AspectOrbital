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
)

func commandInteraction(name string, options []*discordgo.ApplicationCommandInteractionDataOption) *discordgo.InteractionCreate {
	return &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionApplicationCommand,
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
	var captured spokeCommandRequest

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

	bridge := &spokeCommandBridge{
		log:        log.New(io.Discard, "", 0),
		httpClient: server.Client(),
		commandURL: server.URL,
		commands: map[string]spokeCommandSpec{
			"status": {Name: "status"},
		},
	}

	var messages []string
	prev := respondEphemeralFunc
	respondEphemeralFunc = func(_ *discordgo.Session, _ *discordgo.Interaction, message string) error {
		messages = append(messages, message)
		return nil
	}
	t.Cleanup(func() {
		respondEphemeralFunc = prev
	})

	handler := interactionHandler(log.New(io.Discard, "", 0), bridge)
	handler(nil, commandInteraction("status", []*discordgo.ApplicationCommandInteractionDataOption{{Name: "argument", Value: " 30m "}}))

	if len(messages) != 1 || messages[0] != "from spoke" {
		t.Fatalf("unexpected response messages: %#v", messages)
	}
	if captured.Command != "status" {
		t.Fatalf("unexpected command forwarded to spoke: %q", captured.Command)
	}
	if got := strings.TrimSpace(captured.Argument); got != "30m" {
		t.Fatalf("expected forwarded argument 30m, got %q", captured.Argument)
	}
}
