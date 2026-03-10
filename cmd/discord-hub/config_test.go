package main

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func clearHubEnv(t *testing.T) {
	t.Helper()

	keys := []string{
		"DISCORD_BOT_TOKEN",
		"DISCORD_GUILD_ID",
		"HUB_HTTP_ADDR",
		"HUB_NOTIFY_AUTH_TOKEN",
		"HUB_CALLBACK_AUTH_TOKEN",
		"DISCORD_CRITICAL_MENTION",
		"DISCORD_CHANNEL_MAP",
		"SPOKE_COMMANDS_ENABLED",
		"SPOKE_COMMAND_SERVICES",
	}

	for _, key := range keys {
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("failed to unset %s: %v", key, err)
		}
	}
}

func setHubRequiredEnv(t *testing.T) {
	t.Helper()

	t.Setenv("DISCORD_BOT_TOKEN", "Bot token")
	t.Setenv("DISCORD_GUILD_ID", "guild-1")
	t.Setenv("HUB_HTTP_ADDR", "127.0.0.1:8080")
	t.Setenv("HUB_NOTIFY_AUTH_TOKEN", "test-notify-token")
	t.Setenv("HUB_CALLBACK_AUTH_TOKEN", "test-callback-token")
	t.Setenv("DISCORD_CRITICAL_MENTION", "<@123>")
	t.Setenv("SPOKE_COMMANDS_ENABLED", "false")
}

func TestLoadHubConfigRequiresDiscordBotToken(t *testing.T) {
	clearHubEnv(t)
	setHubRequiredEnv(t)
	if err := os.Unsetenv("DISCORD_BOT_TOKEN"); err != nil {
		t.Fatalf("failed to unset DISCORD_BOT_TOKEN: %v", err)
	}

	_, err := loadHubConfig()
	if err == nil {
		t.Fatal("expected error for missing DISCORD_BOT_TOKEN")
	}
	if !strings.Contains(err.Error(), "required key DISCORD_BOT_TOKEN missing value") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadHubConfigAddsBotPrefixWhenMissing(t *testing.T) {
	clearHubEnv(t)
	setHubRequiredEnv(t)
	t.Setenv("DISCORD_BOT_TOKEN", "abc123")
	t.Setenv("DISCORD_CHANNEL_MAP", "alerts:111")
	t.Setenv("SPOKE_COMMANDS_ENABLED", "true")
	t.Setenv("SPOKE_COMMAND_SERVICES", `[{"name":"beeminder-spoke","commandsUrl":"http://127.0.0.1:8090/control/commands","executeUrl":"http://127.0.0.1:8090/control/command"}]`)

	cfg, err := loadHubConfig()
	if err != nil {
		t.Fatalf("loadHubConfig returned error: %v", err)
	}

	if cfg.DiscordToken != "Bot abc123" {
		t.Fatalf("unexpected token: %q", cfg.DiscordToken)
	}
}

func TestLoadHubConfigRequiresHTTPAddrWhenUnset(t *testing.T) {
	clearHubEnv(t)
	setHubRequiredEnv(t)
	if err := os.Unsetenv("HUB_HTTP_ADDR"); err != nil {
		t.Fatalf("failed to unset HUB_HTTP_ADDR: %v", err)
	}

	_, err := loadHubConfig()
	if err == nil {
		t.Fatal("expected error for missing HUB_HTTP_ADDR")
	}
	if !strings.Contains(err.Error(), "required key HUB_HTTP_ADDR missing value") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadHubConfigRequiresNotifyAuthTokenWhenUnset(t *testing.T) {
	clearHubEnv(t)
	setHubRequiredEnv(t)
	if err := os.Unsetenv("HUB_NOTIFY_AUTH_TOKEN"); err != nil {
		t.Fatalf("failed to unset HUB_NOTIFY_AUTH_TOKEN: %v", err)
	}

	_, err := loadHubConfig()
	if err == nil {
		t.Fatal("expected error for missing HUB_NOTIFY_AUTH_TOKEN")
	}
	if !strings.Contains(err.Error(), "required key HUB_NOTIFY_AUTH_TOKEN missing value") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadHubConfigRequiresCallbackAuthTokenWhenUnset(t *testing.T) {
	clearHubEnv(t)
	setHubRequiredEnv(t)
	if err := os.Unsetenv("HUB_CALLBACK_AUTH_TOKEN"); err != nil {
		t.Fatalf("failed to unset HUB_CALLBACK_AUTH_TOKEN: %v", err)
	}

	_, err := loadHubConfig()
	if err == nil {
		t.Fatal("expected error for missing HUB_CALLBACK_AUTH_TOKEN")
	}
	if !strings.Contains(err.Error(), "required key HUB_CALLBACK_AUTH_TOKEN missing value") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildChannelMapParsesDynamicMappings(t *testing.T) {
	clearHubEnv(t)
	t.Setenv("DISCORD_CHANNEL_MAP", " custom-one : 222 , malformed ,custom-two:333, mandarin-streaks:444, :oops, nope:, kalshi-alerts:555 ")

	got := buildChannelMap()
	want := map[string]string{
		"kalshi-alerts":    "555",
		"mandarin-streaks": "444",
		"custom-one":       "222",
		"custom-two":       "333",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected channel map\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestBuildChannelMapIgnoresMalformedPairsAndEmptyValues(t *testing.T) {
	clearHubEnv(t)
	t.Setenv("DISCORD_CHANNEL_MAP", "badpair, :123, empty:, ok:456")

	got := buildChannelMap()
	want := map[string]string{"ok": "456"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected channel map\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestLoadHubConfigRequiresSpokeCommandsEnabled(t *testing.T) {
	clearHubEnv(t)
	setHubRequiredEnv(t)
	if err := os.Unsetenv("SPOKE_COMMANDS_ENABLED"); err != nil {
		t.Fatalf("failed to unset SPOKE_COMMANDS_ENABLED: %v", err)
	}

	_, err := loadHubConfig()
	if err == nil {
		t.Fatal("expected error for missing SPOKE_COMMANDS_ENABLED")
	}
	if !strings.Contains(err.Error(), "required key SPOKE_COMMANDS_ENABLED missing value") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadHubConfigRequiresSpokeBridgeURLsWhenEnabled(t *testing.T) {
	clearHubEnv(t)
	setHubRequiredEnv(t)
	t.Setenv("SPOKE_COMMANDS_ENABLED", "true")

	_, err := loadHubConfig()
	if err == nil {
		t.Fatal("expected error when spoke bridge URLs are missing")
	}
	if !strings.Contains(err.Error(), "SPOKE_COMMAND_SERVICES is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}
