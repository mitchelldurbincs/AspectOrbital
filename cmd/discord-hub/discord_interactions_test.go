package main

import (
	"context"
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
	"personal-infrastructure/pkg/hubnotify"
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

type fakeActionCallbackDispatcher struct {
	callbackURL string
	payload     hubnotify.ActionCallbackRequest
	response    hubnotify.ActionCallbackResponse
	err         error
}

func (f *fakeActionCallbackDispatcher) Dispatch(_ context.Context, callbackURL string, payload hubnotify.ActionCallbackRequest) (hubnotify.ActionCallbackResponse, error) {
	f.callbackURL = callbackURL
	f.payload = payload
	if f.err != nil {
		return hubnotify.ActionCallbackResponse{}, f.err
	}
	return f.response, nil
}

func TestActionCallbackDispatcherSetsBearerToken(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","message":"done"}`))
	}))
	defer server.Close()

	dispatcher := newActionCallbackDispatcher(server.Client(), "test-callback-token")
	_, err := dispatcher.Dispatch(context.Background(), server.URL, hubnotify.ActionCallbackRequest{Version: hubnotify.Version2})
	if err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}
	if authHeader != "Bearer test-callback-token" {
		t.Fatalf("unexpected authorization header: %q", authHeader)
	}
}

func componentInteraction(customID string, message *discordgo.Message) *discordgo.InteractionCreate {
	return &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			ID:        "ix-1",
			Type:      discordgo.InteractionMessageComponent,
			GuildID:   "g-456",
			ChannelID: "c-789",
			Member:    &discordgo.Member{User: &discordgo.User{ID: "u-123"}},
			Message:   message,
			Data:      discordgo.MessageComponentInteractionData{CustomID: customID},
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

	handler := interactionHandler(log.New(io.Discard, "", 0), nil, nil)
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
	runtime := newBridgeRuntime()
	runtime.storeSyncResult(bridge, nil)

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

	handler := interactionHandler(log.New(io.Discard, "", 0), runtime, nil)
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

	handler := interactionHandler(log.New(io.Discard, "", 0), nil, nil)
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
	runtime := newBridgeRuntime()
	runtime.storeSyncResult(bridge, nil)

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

	handler := interactionHandler(log.New(io.Discard, "", 0), runtime, nil)
	handler(nil, commandInteraction("status", nil))

	if len(messages) != 1 {
		t.Fatalf("expected one response message, got %#v", messages)
	}
	if messages[0] != "Command failed: execution failed" {
		t.Fatalf("unexpected failure message: %q", messages[0])
	}
}

func TestInteractionHandlerRespondsWhenCommandsAreSyncing(t *testing.T) {
	var messages []string

	prev := respondEphemeralFunc
	respondEphemeralFunc = func(_ *discordgo.Session, _ *discordgo.Interaction, message string) error {
		messages = append(messages, message)
		return nil
	}
	t.Cleanup(func() {
		respondEphemeralFunc = prev
	})

	runtime := newBridgeRuntime()
	runtime.markSyncStarted()

	handler := interactionHandler(log.New(io.Discard, "", 0), runtime, nil)
	handler(nil, commandInteraction("status", nil))

	if len(messages) != 1 {
		t.Fatalf("expected exactly one response, got %d", len(messages))
	}
	if messages[0] != spokeCommandsSyncingMessage {
		t.Fatalf("unexpected syncing message: %q", messages[0])
	}
}

func TestInteractionHandlerRoutesButtonCallbacks(t *testing.T) {
	dispatcher := &fakeActionCallbackDispatcher{response: hubnotify.ActionCallbackResponse{Status: "ok", Message: "Snoozed for 10 minutes."}}
	var messages []string

	prevDefer := deferEphemeralFunc
	deferEphemeralFunc = func(_ *discordgo.Session, _ *discordgo.Interaction) error { return nil }
	prevFollowup := followupEphemeralFunc
	followupEphemeralFunc = func(_ *discordgo.Session, _ *discordgo.Interaction, message string) error {
		messages = append(messages, message)
		return nil
	}
	t.Cleanup(func() {
		deferEphemeralFunc = prevDefer
		followupEphemeralFunc = prevFollowup
	})

	customID := encodeNotifyActionCustomID("http://127.0.0.1:8092/callback", "beeminder-spoke", "goal-reminder", "snooze_10m")
	message := &discordgo.Message{
		ID: "m-1",
		Embeds: []*discordgo.MessageEmbed{{
			Title: "[BEEMINDER-SPOKE] GOAL REMINDER",
			URL:   "https://example.com/goal",
			Color: notifySeverityColors[hubnotify.SeverityWarning],
		}},
		Components: []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{Label: "Snooze 10m", Style: discordgo.SecondaryButton, CustomID: customID},
		}}},
	}

	handler := interactionHandler(log.New(io.Discard, "", 0), nil, dispatcher)
	handler(nil, componentInteraction(customID, message))

	if dispatcher.callbackURL != "http://127.0.0.1:8092/callback" {
		t.Fatalf("unexpected callback URL: %q", dispatcher.callbackURL)
	}
	if dispatcher.payload.Action.ID != "snooze_10m" || dispatcher.payload.Action.Label != "Snooze 10m" {
		t.Fatalf("unexpected action payload: %#v", dispatcher.payload.Action)
	}
	if dispatcher.payload.Context.DiscordUserID != "u-123" || dispatcher.payload.Context.MessageID != "m-1" {
		t.Fatalf("unexpected callback context: %#v", dispatcher.payload.Context)
	}
	if len(messages) != 1 || messages[0] != "Snoozed for 10 minutes." {
		t.Fatalf("unexpected followup messages: %#v", messages)
	}
}

func TestInteractionHandlerRejectsInvalidButtonCustomID(t *testing.T) {
	var messages []string
	prevRespond := respondEphemeralFunc
	respondEphemeralFunc = func(_ *discordgo.Session, _ *discordgo.Interaction, message string) error {
		messages = append(messages, message)
		return nil
	}
	t.Cleanup(func() { respondEphemeralFunc = prevRespond })

	handler := interactionHandler(log.New(io.Discard, "", 0), nil, &fakeActionCallbackDispatcher{})
	handler(nil, componentInteraction("bad-custom-id", &discordgo.Message{}))

	if len(messages) != 1 || messages[0] != "That action is invalid." {
		t.Fatalf("unexpected messages: %#v", messages)
	}
}
