package main

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"

	spokebridge "personal-infrastructure/cmd/discord-hub/spoke_bridge"
)

const testDiscordResponseCharacterLimit = 1900

func TestParseSpokeCommandCatalogSupportsModernPayload(t *testing.T) {
	body := []byte(`{"version":1,"service":"beeminder-spoke","commands":[{"name":"status","description":"Show status"}]}`)

	commands, err := spokebridge.ParseCommandCatalog(body)
	if err != nil {
		t.Fatalf("parseSpokeCommandCatalog returned error: %v", err)
	}
	if len(commands) != 1 || commands[0].Name != "status" {
		t.Fatalf("unexpected parsed commands: %#v", commands)
	}
}

func TestParseSpokeCommandCatalogRejectsUnrecognizedPayload(t *testing.T) {
	_, err := spokebridge.ParseCommandCatalog([]byte(`{"commands":[]}`))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestParseSpokeCommandCatalogRejectsLegacyPayload(t *testing.T) {
	_, err := spokebridge.ParseCommandCatalog([]byte(`{"commands":["started","snooze"]}`))
	if err == nil {
		t.Fatal("expected error for legacy command payload")
	}
}

func TestNormalizeSpokeCommandSpecsFiltersDedupesAndSkipsPing(t *testing.T) {
	input := []spokebridge.CommandSpec{
		{Name: "", Description: "empty"},
		{Name: pingCommandName, Description: "reserved"},
		{
			Name:        " STATUS ",
			Description: "",
			Options: []spokebridge.CommandOptionSpec{
				{Name: "Argument", Type: " string ", Description: "", Required: false},
				{Name: "Argument", Type: "int", Description: "duplicate", Required: true},
				{Name: "bad name", Type: "bool", Description: "invalid"},
			},
		},
		{Name: "status", Description: "duplicate should be ignored"},
		{Name: "bad name", Description: "invalid"},
		{Name: "resume", Description: "Resume reminders"},
	}

	got := spokebridge.NormalizeCommandSpecs(input)
	if len(got) != 2 {
		t.Fatalf("expected 2 normalized commands, got %d (%#v)", len(got), got)
	}
	if got[0].Name != "resume" || got[1].Name != "status" {
		t.Fatalf("expected sorted command names [resume status], got [%s %s]", got[0].Name, got[1].Name)
	}
	if got[1].Description != "Command owned by configured spoke service" {
		t.Fatalf("unexpected fallback description: %q", got[1].Description)
	}
	if len(got[1].Options) != 1 {
		t.Fatalf("expected one normalized option, got %#v", got[1].Options)
	}
	if got[1].Options[0].Type != "string" {
		t.Fatalf("unexpected normalized option type: %q", got[1].Options[0].Type)
	}
}

func TestNormalizeSpokeOptionType(t *testing.T) {
	tests := map[string]string{
		"":           "string",
		"string":     "string",
		"int":        "integer",
		"integer":    "integer",
		"float64":    "number",
		"bool":       "boolean",
		"attachment": "attachment",
		"weirdtype":  "",
	}

	for input, want := range tests {
		if got := spokebridge.NormalizeOptionType(input); got != want {
			t.Fatalf("normalizeSpokeOptionType(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestDiscordOptionTypeMappings(t *testing.T) {
	if got := spokebridge.DiscordOptionType("integer"); got != discordgo.ApplicationCommandOptionInteger {
		t.Fatalf("expected integer mapping, got %v", got)
	}
	if got := spokebridge.DiscordOptionType("number"); got != discordgo.ApplicationCommandOptionNumber {
		t.Fatalf("expected number mapping, got %v", got)
	}
	if got := spokebridge.DiscordOptionType("boolean"); got != discordgo.ApplicationCommandOptionBoolean {
		t.Fatalf("expected boolean mapping, got %v", got)
	}
	if got := spokebridge.DiscordOptionType("attachment"); got != discordgo.ApplicationCommandOptionAttachment {
		t.Fatalf("expected attachment mapping, got %v", got)
	}
	if got := spokebridge.DiscordOptionType("anything-else"); got != discordgo.ApplicationCommandOptionString {
		t.Fatalf("expected fallback string mapping, got %v", got)
	}
}

func TestPruneCommandOptionsNormalizesAndFilters(t *testing.T) {
	input := map[string]any{
		" Argument ": " 30m ",
		"Flag":       true,
		"Count":      int64(2),
		"Number":     json.Number("3.5"),
		"bad key":    "ignored",
		"":           "ignored",
		"list":       []int{1, 2},
		"proof":      map[string]any{"id": "a1"},
	}

	got := spokebridge.PruneCommandOptions(input)

	if got["argument"] != "30m" {
		t.Fatalf("expected trimmed argument option, got %#v", got["argument"])
	}
	if got["flag"] != true {
		t.Fatalf("expected bool option true, got %#v", got["flag"])
	}
	if got["count"] != int64(2) {
		t.Fatalf("expected int64 option 2, got %#v", got["count"])
	}
	if got["number"] != json.Number("3.5") {
		t.Fatalf("expected json.Number option 3.5, got %#v", got["number"])
	}
	if got["proof"].(map[string]any)["id"] != "a1" {
		t.Fatalf("expected proof metadata map to pass through, got %#v", got["proof"])
	}
	if got["list"] != "[1 2]" {
		t.Fatalf("expected fmt string conversion for list, got %#v", got["list"])
	}
	if _, exists := got["bad key"]; exists {
		t.Fatalf("expected invalid key to be removed, got %#v", got)
	}
}

func TestTruncateForDiscord(t *testing.T) {
	short := "hello"
	if got := spokebridge.TruncateForDiscord(short); got != short {
		t.Fatalf("expected unchanged short message, got %q", got)
	}

	exact := strings.Repeat("a", testDiscordResponseCharacterLimit)
	if got := spokebridge.TruncateForDiscord(exact); got != exact {
		t.Fatalf("expected unchanged exact-limit message, got len=%d", len(got))
	}

	over := strings.Repeat("b", testDiscordResponseCharacterLimit+10)
	got := spokebridge.TruncateForDiscord(over)
	if len(got) != testDiscordResponseCharacterLimit {
		t.Fatalf("expected truncated length %d, got %d", testDiscordResponseCharacterLimit, len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected truncated message to end with ellipsis, got %q", got[len(got)-3:])
	}
}

func TestBuildDiscordCommandsUsesSortedCommandNames(t *testing.T) {
	bridge := spokebridge.NewBridge(nil, nil, "", "", map[string]spokebridge.CommandSpec{
		"status": {
			Name:        "status",
			Description: "status desc",
		},
		"resume": {
			Name:        "resume",
			Description: "resume desc",
		},
	})

	got := bridge.BuildDiscordCommands()
	if len(got) != 2 {
		t.Fatalf("expected two commands, got %d", len(got))
	}
	names := []string{got[0].Name, got[1].Name}
	want := []string{"resume", "status"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("unexpected command order\nwant: %#v\ngot:  %#v", want, names)
	}
}

func TestFormatSpokeCommandFailure(t *testing.T) {
	if got := spokebridge.FormatCommandFailure(nil); got != "" {
		t.Fatalf("expected empty string for nil error, got %q", got)
	}

	if got := spokebridge.FormatCommandFailure(errors.New("boom")); got != "Command failed: boom" {
		t.Fatalf("unexpected failure formatting: %q", got)
	}
}
