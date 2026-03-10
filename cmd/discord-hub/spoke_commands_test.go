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

func TestParseSpokeCommandCatalogRejectsOptionTypeAliases(t *testing.T) {
	_, err := spokebridge.ParseCommandCatalog([]byte(`{"version":1,"service":"beeminder-spoke","commands":[{"name":"status","description":"Show status","options":[{"name":"duration","type":"int","description":"Bad alias"}]}]}`))
	if err == nil {
		t.Fatal("expected error for aliased option type")
	}
	if !strings.Contains(err.Error(), `invalid option type "int"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNormalizeSpokeCommandSpecsRejectsReservedPing(t *testing.T) {
	input := []spokebridge.CommandSpec{
		{Name: pingCommandName, Description: "reserved"},
	}

	_, err := spokebridge.NormalizeCommandSpecs(input)
	if err == nil {
		t.Fatal("expected reserved command error")
	}
	if !strings.Contains(err.Error(), `command "ping" is reserved`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNormalizeSpokeOptionType(t *testing.T) {
	tests := map[string]string{
		"string":     "string",
		"integer":    "integer",
		"number":     "number",
		"boolean":    "boolean",
		"attachment": "attachment",
		"int":        "",
		"float64":    "",
		"bool":       "",
		"":           "",
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
		t.Fatalf("unexpected option mapping for unsupported type: %v", got)
	}
}

func TestPruneCommandOptionsPreservesExactKeysAndValues(t *testing.T) {
	input := map[string]any{
		"argument": " 30m ",
		"flag":     true,
		"count":    int64(2),
		"number":   json.Number("3.5"),
		"proof":    map[string]any{"id": "a1"},
	}

	got, err := spokebridge.PruneCommandOptions(input)
	if err != nil {
		t.Fatalf("PruneCommandOptions returned error: %v", err)
	}

	if got["argument"] != " 30m " {
		t.Fatalf("expected exact argument option, got %#v", got["argument"])
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
}

func TestPruneCommandOptionsRejectsInvalidKeysAndTypes(t *testing.T) {
	_, err := spokebridge.PruneCommandOptions(map[string]any{"bad key": "ignored"})
	if err == nil {
		t.Fatal("expected invalid option key error")
	}
	if !strings.Contains(err.Error(), `option name "bad key" is invalid`) {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = spokebridge.PruneCommandOptions(map[string]any{"list": []int{1, 2}})
	if err == nil {
		t.Fatal("expected unsupported option value error")
	}
	if !strings.Contains(err.Error(), `option "list" has unsupported value type []int`) {
		t.Fatalf("unexpected error: %v", err)
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
	bridge, err := spokebridge.NewBridge(nil, nil, "", "http://127.0.0.1:8090/control/commands", "http://127.0.0.1:8090/control/command", map[string]spokebridge.CommandSpec{
		"status": {
			Name:        "status",
			Description: "status desc",
		},
		"resume": {
			Name:        "resume",
			Description: "resume desc",
		},
	})
	if err != nil {
		t.Fatalf("NewBridge returned error: %v", err)
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
	if got := spokebridge.FormatCommandFailure(nil); got != "" {
		t.Fatalf("expected empty string for nil error, got %q", got)
	}

	if got := spokebridge.FormatCommandFailure(errors.New("boom")); got != "Command failed: boom" {
		t.Fatalf("unexpected failure formatting: %q", got)
	}
}
