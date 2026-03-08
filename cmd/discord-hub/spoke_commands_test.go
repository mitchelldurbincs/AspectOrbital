package main

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestParseSpokeCommandCatalogSupportsModernPayload(t *testing.T) {
	body := []byte(`{"version":1,"service":"beeminder-spoke","commands":[{"name":"status","description":"Show status"}]}`)

	commands, err := parseSpokeCommandCatalog(body)
	if err != nil {
		t.Fatalf("parseSpokeCommandCatalog returned error: %v", err)
	}
	if len(commands) != 1 || commands[0].Name != "status" {
		t.Fatalf("unexpected parsed commands: %#v", commands)
	}
}

func TestParseSpokeCommandCatalogSupportsLegacyPayload(t *testing.T) {
	body := []byte(`{"commands":["started","snooze"]}`)

	commands, err := parseSpokeCommandCatalog(body)
	if err != nil {
		t.Fatalf("parseSpokeCommandCatalog returned error: %v", err)
	}

	if len(commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(commands))
	}
	if commands[0].Description != legacySpokeCommandDescription {
		t.Fatalf("unexpected legacy description: %q", commands[0].Description)
	}
	if len(commands[0].Options) != 1 || commands[0].Options[0].Name != legacySpokeArgumentOption {
		t.Fatalf("unexpected legacy options: %#v", commands[0].Options)
	}
}

func TestParseSpokeCommandCatalogRejectsUnrecognizedPayload(t *testing.T) {
	_, err := parseSpokeCommandCatalog([]byte(`{"commands":[]}`))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestNormalizeSpokeCommandSpecsFiltersDedupesAndSkipsPing(t *testing.T) {
	input := []spokeCommandSpec{
		{Name: "", Description: "empty"},
		{Name: pingCommandName, Description: "reserved"},
		{
			Name:        " STATUS ",
			Description: "",
			Options: []spokeCommandOptionSpec{
				{Name: "Argument", Type: " string ", Description: "", Required: false},
				{Name: "Argument", Type: "int", Description: "duplicate", Required: true},
				{Name: "bad name", Type: "bool", Description: "invalid"},
			},
		},
		{Name: "status", Description: "duplicate should be ignored"},
		{Name: "bad name", Description: "invalid"},
		{Name: "resume", Description: "Resume reminders"},
	}

	got := normalizeSpokeCommandSpecs(input)
	if len(got) != 2 {
		t.Fatalf("expected 2 normalized commands, got %d (%#v)", len(got), got)
	}
	if got[0].Name != "resume" || got[1].Name != "status" {
		t.Fatalf("expected sorted command names [resume status], got [%s %s]", got[0].Name, got[1].Name)
	}
	if got[1].Description != legacySpokeCommandDescription {
		t.Fatalf("expected fallback description %q, got %q", legacySpokeCommandDescription, got[1].Description)
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
		"":          "string",
		"string":    "string",
		"int":       "integer",
		"integer":   "integer",
		"float64":   "number",
		"bool":      "boolean",
		"weirdtype": "string",
	}

	for input, want := range tests {
		if got := normalizeSpokeOptionType(input); got != want {
			t.Fatalf("normalizeSpokeOptionType(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestDiscordOptionTypeMappings(t *testing.T) {
	if got := discordOptionType("integer"); got != discordgo.ApplicationCommandOptionInteger {
		t.Fatalf("expected integer mapping, got %v", got)
	}
	if got := discordOptionType("number"); got != discordgo.ApplicationCommandOptionNumber {
		t.Fatalf("expected number mapping, got %v", got)
	}
	if got := discordOptionType("boolean"); got != discordgo.ApplicationCommandOptionBoolean {
		t.Fatalf("expected boolean mapping, got %v", got)
	}
	if got := discordOptionType("anything-else"); got != discordgo.ApplicationCommandOptionString {
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
	}

	got := pruneCommandOptions(input)

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
	if got["list"] != "[1 2]" {
		t.Fatalf("expected fmt string conversion for list, got %#v", got["list"])
	}
	if _, exists := got["bad key"]; exists {
		t.Fatalf("expected invalid key to be removed, got %#v", got)
	}
}

func TestTruncateForDiscord(t *testing.T) {
	short := "hello"
	if got := truncateForDiscord(short); got != short {
		t.Fatalf("expected unchanged short message, got %q", got)
	}

	exact := strings.Repeat("a", discordResponseCharacterLimit)
	if got := truncateForDiscord(exact); got != exact {
		t.Fatalf("expected unchanged exact-limit message, got len=%d", len(got))
	}

	over := strings.Repeat("b", discordResponseCharacterLimit+10)
	got := truncateForDiscord(over)
	if len(got) != discordResponseCharacterLimit {
		t.Fatalf("expected truncated length %d, got %d", discordResponseCharacterLimit, len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected truncated message to end with ellipsis, got %q", got[len(got)-3:])
	}
}

func TestBuildDiscordCommandsUsesSortedCommandNames(t *testing.T) {
	bridge := &spokeCommandBridge{
		commands: map[string]spokeCommandSpec{
			"status": {
				Name:        "status",
				Description: "status desc",
			},
			"resume": {
				Name:        "resume",
				Description: "resume desc",
			},
		},
	}

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
	if got := formatSpokeCommandFailure(nil); got != "" {
		t.Fatalf("expected empty string for nil error, got %q", got)
	}

	if got := formatSpokeCommandFailure(errors.New("boom")); got != "Command failed: boom" {
		t.Fatalf("unexpected failure formatting: %q", got)
	}
}
